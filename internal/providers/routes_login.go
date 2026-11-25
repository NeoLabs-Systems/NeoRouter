package providers

// The device-code sign-in routes of the signed-in providers: ChatGPT (Codex)
// and Nous Portal. A sign-in belongs to the space that started it, and with an
// accountId an existing connection is signed in again instead of a new one
// being created (server/routes/providers.js).

import (
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// loginBillingModes are the billing modes a ChatGPT sign-in accepts; anything
// else starts a plan connection.
var loginBillingModes = []string{"plan", "plan_paid", "paid"}

// codexPendingBody is the answer of a sign-in that is not finished yet.
type codexPendingBody struct {
	Status string `json:"status"`
}

// codexDoneBody is the answer of a finished ChatGPT sign-in.
type codexDoneBody struct {
	Status  string        `json:"status"`
	Plan    *string       `json:"plan"`
	Account PublicAccount `json:"account"`
}

// nousPendingBody is the answer of a Nous sign-in that is not finished yet;
// Interval is set once the Portal asked for a longer wait.
type nousPendingBody struct {
	Status   string `json:"status"`
	Interval *int   `json:"interval"`
}

// nousDoneBody is the answer of a finished Nous sign-in.
type nousDoneBody struct {
	Status  string        `json:"status"`
	Email   *string       `json:"email"`
	Tier    *string       `json:"tier"`
	Account PublicAccount `json:"account"`
}

// chatgptStart starts a ChatGPT sign-in with a device code.
func (rt *routes) chatgptStart(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var body struct {
		AccountID   string `json:"accountId"`
		BillingMode string `json:"billingMode"`
	}
	if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	login, err := rt.oauthFlows().StartCodexLogin(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	rt.storeLogin(rt.chatgptLogins, login.LoginID, pendingLogin{
		ownerID:     space.OwnerID,
		actorID:     space.ActorID,
		accountID:   body.AccountID,
		billingMode: billingModeForLogin(body.BillingMode, loginBillingModes...),
	})
	httpx.WriteJSON(w, http.StatusOK, login)
}

// chatgptPoll is one poll of a started ChatGPT sign-in: pending until the user
// approved it at auth.openai.com/codex/device, then the connection's account.
func (rt *routes) chatgptPoll(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	loginID := r.PathValue("loginId")
	pending, ok := rt.takeLogin(rt.chatgptLogins, loginID, space.OwnerID)
	if !ok {
		httpserver.WriteError(w, r, rt.Log, loginNotFound())
		return
	}
	result, err := rt.oauthFlows().PollCodexLogin(r.Context(), loginID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if result.Status != "done" {
		if result.Status == "expired" {
			rt.forgetLogin(rt.chatgptLogins, loginID)
		}
		httpx.WriteJSON(w, http.StatusOK, codexPendingBody{Status: result.Status})
		return
	}
	rt.forgetLogin(rt.chatgptLogins, loginID)
	account, err := rt.signInAccount(r, pending, result.Secret, "chatgpt", pending.billingMode)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	rt.syncNewAccount(r.Context(), account.ID)
	fresh, err := rt.Get(r.Context(), pending.ownerID, account.ID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, codexDoneBody{Status: "done", Plan: result.Plan, Account: fresh})
}

// nousStart starts a Nous Portal sign-in with a device code.
func (rt *routes) nousStart(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var body struct {
		AccountID string `json:"accountId"`
	}
	if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	login, err := rt.oauthFlows().StartNousLogin(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, nousSignInError(err))
		return
	}
	rt.storeLogin(rt.nousLogins, login.LoginID, pendingLogin{
		ownerID:   space.OwnerID,
		actorID:   space.ActorID,
		accountID: body.AccountID,
	})
	rt.forgetPoll(login.LoginID)
	httpx.WriteJSON(w, http.StatusOK, login)
}

// nousPoll is one poll of a started Nous Portal sign-in. A refusal or a failed
// poll is reported as it is, and the sign-in is dropped.
func (rt *routes) nousPoll(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	loginID := r.PathValue("loginId")
	pending, ok := rt.takeLogin(rt.nousLogins, loginID, space.OwnerID)
	if !ok {
		httpserver.WriteError(w, r, rt.Log, loginNotFound())
		return
	}
	result, err := rt.oauthFlows().PollNousLogin(r.Context(), loginID)
	if err != nil {
		rt.forgetLogin(rt.nousLogins, loginID)
		httpserver.WriteError(w, r, rt.Log, nousSignInError(err))
		return
	}
	if result.Status != "done" {
		if result.Status == "expired" {
			rt.forgetLogin(rt.nousLogins, loginID)
		}
		// slow_down: the Portal asked for a longer interval, so the app must wait.
		if result.Status == "slow_down" {
			rt.slowDown(loginID)
		}
		status := result.Status
		if status == "slow_down" {
			status = "pending"
		}
		httpx.WriteJSON(w, http.StatusOK, nousPendingBody{Status: status, Interval: rt.pollInterval(loginID)})
		return
	}
	rt.forgetLogin(rt.nousLogins, loginID)
	rt.forgetPoll(loginID)
	account, err := rt.signInAccount(r, pending, result.Secret, "nous", "paid")
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	rt.syncNewAccount(r.Context(), account.ID)
	fresh, err := rt.Get(r.Context(), pending.ownerID, account.ID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nousDoneBody{
		Status:  "done",
		Email:   textOrNil(&result.Email),
		Tier:    textOrNil(&result.Tier),
		Account: fresh,
	})
}

// signInAccount creates the connection a finished sign-in is stored as, or
// re-keys the one it was started for. A sign-in is a trusted local caller: it
// carries no actor for the checks on where a key may be sent.
func (rt *routes) signInAccount(r *http.Request, pending pendingLogin, secret, definitionID, billingMode string) (PublicAccount, error) {
	if pending.accountID != "" {
		return rt.Service.Update(r.Context(), pending.ownerID, pending.accountID, secretInput(secret), nil)
	}
	created, err := rt.Create(r.Context(), pending.ownerID, definitionInput(definitionID, secret, billingMode), pending.actorID, nil)
	if err != nil {
		return PublicAccount{}, err
	}
	return created.Account, nil
}

// slowDown records that the Portal asked for a longer polling interval, five
// seconds at a time.
func (rt *routes) slowDown(loginID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.nousPolls[loginID] += 5
	if rt.nousPolls[loginID] < 10 {
		rt.nousPolls[loginID] = 10
	}
}

// pollInterval is the interval a client should use, nil when the Portal never
// asked for one.
func (rt *routes) pollInterval(loginID string) *int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	value, ok := rt.nousPolls[loginID]
	if !ok {
		return nil
	}
	return &value
}

// forgetPoll drops a slowed-down interval.
func (rt *routes) forgetPoll(loginID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.nousPolls, loginID)
}
