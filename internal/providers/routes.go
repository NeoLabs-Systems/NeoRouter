package providers

// The providers API at /api/v1/providers.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Space says whose providers a request works on and what the caller may do
// there (as internal/teams resolves it).
type Space struct {
	OwnerID string
	ActorID string
	IsOwner bool
	// Role is the signed-in account's role: the checks on where a key may be
	// sent need it.
	Role string
}

// RouteDeps is what the providers routes need from the request context.
type RouteDeps struct {
	// Space resolves the space a request works on and the person acting in it.
	Space func(r *http.Request) (Space, bool)
	// Require wraps a handler with the permission check on the request's space.
	Require func(permission string) func(http.Handler) http.Handler
}

// routes is the router of the providers API: the service plus the state the
// handlers keep (the sign-ins started but not finished).
type routes struct {
	*Service
	deps RouteDeps

	importLimiter *httpserver.SlidingWindow

	mu            sync.Mutex
	chatgptLogins map[string]pendingLogin
	nousLogins    map[string]pendingLogin
	nousPolls     map[string]int
}

// pendingLogin is a started sign-in that belongs to the space that started it.
type pendingLogin struct {
	ownerID     string
	actorID     string
	accountID   string
	billingMode string
}

// Routes registers the providers API.
func (s *Service) Routes(mux *http.ServeMux, deps RouteDeps) {
	rt := &routes{
		Service:       s,
		deps:          deps,
		chatgptLogins: map[string]pendingLogin{},
		nousLogins:    map[string]pendingLogin{},
		nousPolls:     map[string]int{},
	}
	rt.importLimiter = httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
		Window: time.Minute,
		Limit:  5,
		Key:    func(r *http.Request) string { return rt.actorID(r) },
		Log:    s.Log,
	})

	mux.HandleFunc("GET /api/v1/providers/definitions", rt.definitions)
	mux.Handle("GET /api/v1/providers/accounts", rt.guard("providers.view", rt.list))
	mux.Handle("POST /api/v1/providers/probe", rt.guard("providers.manage", rt.probe))
	mux.Handle("POST /api/v1/providers/accounts", rt.guard("providers.manage", rt.create))
	mux.Handle("POST /api/v1/providers/accounts/import", rt.guard("providers.manage", rt.importKeys))
	mux.Handle("PATCH /api/v1/providers/accounts/{id}", rt.guard("providers.manage", rt.update))
	mux.Handle("PUT /api/v1/providers/accounts/{id}/limits", rt.guard("providers.manage", rt.setLimits))
	mux.Handle("POST /api/v1/providers/accounts/{id}/mark-fixed", rt.guard("providers.manage", rt.markFixed))
	mux.Handle("DELETE /api/v1/providers/accounts/{id}", rt.guard("providers.delete", rt.remove))
	mux.Handle("POST /api/v1/providers/accounts/{id}/validate", rt.guard("providers.manage", rt.validate))
	mux.Handle("POST /api/v1/providers/accounts/{id}/sync", rt.guard("providers.manage", rt.sync))
	mux.Handle("POST /api/v1/providers/accounts/{id}/quota", rt.guard("providers.view", rt.quota))
	rt.loginRoutes(mux)
}

// guard wraps a handler with a permission check.
func (rt *routes) guard(permission string, handler http.HandlerFunc) http.Handler {
	if rt.deps.Require == nil {
		return handler
	}
	return rt.deps.Require(permission)(handler)
}

// space resolves the request's space, refusing an unauthenticated caller.
func (rt *routes) space(r *http.Request) (Space, error) {
	if rt.deps.Space == nil {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	space, ok := rt.deps.Space(r)
	if !ok || space.ActorID == "" {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return space, nil
}

// actorID is the caller, for the rate limiter's key.
func (rt *routes) actorID(r *http.Request) string {
	space, err := rt.space(r)
	if err != nil {
		return httpxClientIP(r)
	}
	return space.ActorID
}

// actor is who is changing an account, for the checks on where its key may be
// sent.
func actorOf(space Space) *Actor { return &Actor{Role: space.Role, IsOwner: space.IsOwner} }

// definitions is the connect screen's catalog. Pool partners are added by
// joining a pool, not by hand; this server's local models only when there are
// some and an admin lets this person use them.
func (rt *routes) definitions(w http.ResponseWriter, r *http.Request) {
	ownerID := ""
	if space, err := rt.space(r); err == nil {
		ownerID = space.OwnerID
	}
	waits := rt.waits()
	definitions := []PublicDefinition{}
	for _, definition := range rt.Registry.All() {
		if definition.Category == CategoryPool {
			continue
		}
		if definition.ID == LocalDefinitionID && (rt.LocalModels == nil || !rt.LocalModels.Offered(ownerID)) {
			continue
		}
		definitions = append(definitions, rt.Registry.PublicDefinition(definition, waits))
	}
	httpx.WriteJSON(w, http.StatusOK, definitionsBody{Definitions: definitions})
}

type definitionsBody struct {
	Definitions []PublicDefinition `json:"definitions"`
}

func (rt *routes) list(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accounts, err := rt.List(r.Context(), space.OwnerID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, accountsBody{Accounts: accounts})
}

type accountsBody struct {
	Accounts []PublicAccount `json:"accounts"`
}

type accountBody struct {
	Account PublicAccount `json:"account"`
}

type createdBody struct {
	Account    PublicAccount `json:"account"`
	Validation Validation    `json:"validation"`
	Sync       any           `json:"sync"`
}

type validatedBody struct {
	Validation Validation    `json:"validation"`
	Account    PublicAccount `json:"account"`
}

type syncedBody struct {
	Sync    any           `json:"sync"`
	Account PublicAccount `json:"account"`
}

type fixedBody struct {
	Fixed      FixedResult   `json:"fixed"`
	Validation Validation    `json:"validation"`
	Account    PublicAccount `json:"account"`
}

type quotaBody struct {
	Quota []QuotaWindow `json:"quota"`
}

func (rt *routes) probe(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var in Input
	if err := httpserver.ReadJSONManagement(w, r, &in, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := rt.ValidateInput(in, true); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	probed, err := rt.Probe(r.Context(), space.OwnerID, in, actorOf(space))
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, probed)
}

func (rt *routes) create(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var in Input
	if err := httpserver.ReadJSONManagement(w, r, &in, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := rt.ValidateInput(in, true); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if stringField(in.DefinitionID) == LocalDefinitionID && (rt.LocalModels == nil || !rt.LocalModels.Allowed(space.OwnerID)) {
		httpserver.WriteError(w, r, rt.Log, httpserver.New(http.StatusForbidden, "LOCAL_MODELS_NOT_SHARED",
			"An admin has not shared this server’s local models with you."))
		return
	}
	created, err := rt.Create(r.Context(), space.OwnerID, in, space.ActorID, actorOf(space))
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	sync := rt.FinishSetup(r.Context(), created.Account.ID)
	account, err := rt.Get(r.Context(), space.OwnerID, created.Account.ID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdBody{Account: account, Validation: created.Validation, Sync: sync})
}

func (rt *routes) importKeys(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if !rt.importLimiter.Allow(w, r) {
		return
	}
	var file ImportFile
	if err := httpserver.ReadJSONManagement(w, r, &file, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	result, err := rt.ImportKeys(r.Context(), space.OwnerID, space.ActorID, actorOf(space), file)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt *routes) update(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var in Input
	if err := httpserver.ReadJSONManagement(w, r, &in, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := rt.ValidateInput(in, false); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	account, err := rt.Service.Update(r.Context(), space.OwnerID, accountID, in, actorOf(space))
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if rt.Catalog != nil {
		if err := rt.Catalog.ReclassifyAccount(r.Context(), accountID); err != nil {
			httpserver.WriteError(w, r, rt.Log, err)
			return
		}
	}
	if in.BillingMode.Set || in.Secret.Set {
		if _, err := rt.VerifyAccount(r.Context(), accountID); err != nil {
			httpserver.WriteError(w, r, rt.Log, err)
			return
		}
	}
	fresh, err := rt.Get(r.Context(), space.OwnerID, account.ID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, accountBody{Account: fresh})
}

// setLimits states what an account can take; a number wins over everything the
// router infers, null clears it.
func (rt *routes) setLimits(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	if _, err := rt.GetRow(r.Context(), space.OwnerID, accountID); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var patch map[string]*float64
	if err := httpserver.ReadJSONManagement(w, r, &patch, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := validateLimitsPatch(patch); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if err := rt.SetLimits(r.Context(), accountID, patch); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	account, err := rt.Get(r.Context(), space.OwnerID, accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, accountBody{Account: account})
}

// validateLimits checks a limits patch.
func validateLimitsPatch(patch map[string]*float64) error {
	for metric := range patch {
		known := false
		for _, candidate := range limitedMetrics {
			if candidate == metric {
				known = true
				break
			}
		}
		if !known {
			return validationError("Unrecognized key(s) in object: '" + metric + "'")
		}
	}
	for _, metric := range limitedMetrics {
		value, ok := patch[metric]
		if !ok || value == nil {
			continue
		}
		if err := optionalLimit(metric, Optional[float64]{Set: true, Value: value}); err != nil {
			return err
		}
	}
	return nil
}

// markFixed forgets the cooldowns, rests and failures held against a
// connection, checks the key again and gives it fresh chances.
func (rt *routes) markFixed(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	if _, err := rt.GetRow(r.Context(), space.OwnerID, accountID); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	var fixed FixedResult
	if rt.Fixed != nil {
		fixed = rt.Fixed(accountID)
	}
	validation := UnknownKey("")
	probed, err := rt.ValidateAccount(r.Context(), accountID)
	switch {
	case err != nil:
		validation = UnknownKey(err.Error())
	case probed != nil:
		validation = *probed
	}
	account, err := rt.Get(r.Context(), space.OwnerID, accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fixedBody{Fixed: fixed, Validation: validation, Account: account})
}

func (rt *routes) remove(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if err := rt.Remove(r.Context(), space.OwnerID, r.PathValue("id")); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *routes) validate(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	if _, err := rt.GetRow(r.Context(), space.OwnerID, accountID); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	validation := UnknownKey("")
	probed, err := rt.ValidateAccount(r.Context(), accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if probed != nil {
		validation = *probed
	}
	account, err := rt.Get(r.Context(), space.OwnerID, accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, validatedBody{Validation: validation, Account: account})
}

func (rt *routes) sync(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	if _, err := rt.GetRow(r.Context(), space.OwnerID, accountID); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if rt.Catalog == nil {
		httpserver.WriteError(w, r, rt.Log, errNoCatalog)
		return
	}
	result, err := rt.Catalog.SyncAccount(r.Context(), accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	account, err := rt.Get(r.Context(), space.OwnerID, accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, syncedBody{Sync: result, Account: account})
}

func (rt *routes) quota(w http.ResponseWriter, r *http.Request) {
	space, err := rt.space(r)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	accountID := r.PathValue("id")
	if _, err := rt.GetRow(r.Context(), space.OwnerID, accountID); err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	windows, err := rt.ProbeQuota(r.Context(), accountID)
	if err != nil {
		httpserver.WriteError(w, r, rt.Log, err)
		return
	}
	if windows == nil {
		windows = []QuotaWindow{}
	}
	httpx.WriteJSON(w, http.StatusOK, quotaBody{Quota: windows})
}

// loginRoutes registers the device-code sign-in endpoints.
func (rt *routes) loginRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/providers/chatgpt/login", rt.guard("providers.manage", rt.chatgptStart))
	mux.Handle("POST /api/v1/providers/chatgpt/login/{loginId}", rt.guard("providers.manage", rt.chatgptPoll))
	mux.Handle("POST /api/v1/providers/nous/login", rt.guard("providers.manage", rt.nousStart))
	mux.Handle("POST /api/v1/providers/nous/login/{loginId}", rt.guard("providers.manage", rt.nousPoll))
}

// loginNotFound is the 404 of a sign-in this space never started.
func loginNotFound() error {
	return httpserver.New(http.StatusNotFound, "LOGIN_NOT_FOUND", "This sign-in expired. Start again.")
}

// takeLogin reads a pending sign-in that belongs to the space.
func (rt *routes) takeLogin(logins map[string]pendingLogin, loginID, ownerID string) (pendingLogin, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	pending, ok := logins[loginID]
	if !ok || pending.ownerID != ownerID {
		return pendingLogin{}, false
	}
	return pending, true
}

// forgetLogin drops a pending sign-in.
func (rt *routes) forgetLogin(logins map[string]pendingLogin, loginID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(logins, loginID)
}

// storeLogin keeps a pending sign-in.
func (rt *routes) storeLogin(logins map[string]pendingLogin, loginID string, pending pendingLogin) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	logins[loginID] = pending
}

// nousSignInError tells a refused or failed Nous sign-in to the person as it is,
// not as an internal error.
func nousSignInError(err error) error {
	classified := AsProviderError(err)
	if classified == nil {
		return err
	}
	status := http.StatusBadGateway
	if classified.Kind == KindAuth {
		status = http.StatusBadRequest
	}
	return httpserver.New(status, "NOUS_SIGN_IN_FAILED", classified.Message)
}

// secretInput is a patch that only replaces a connection's key.
func secretInput(secret string) Input {
	return Input{Secret: Optional[string]{Set: true, Value: &secret}}
}

// definitionInput is a new connection of one provider with a key.
func definitionInput(definitionID, secret, billingMode string) Input {
	return Input{
		DefinitionID: Optional[string]{Set: true, Value: &definitionID},
		Secret:       Optional[string]{Set: true, Value: &secret},
		BillingMode:  Optional[string]{Set: true, Value: &billingMode},
	}
}

// syncNewAccount lists a signed-in connection's models, reporting a failure
// without failing the sign-in.
func (rt *routes) syncNewAccount(ctx context.Context, accountID string) {
	if rt.Catalog == nil {
		return
	}
	if _, err := rt.Catalog.SyncAccount(ctx, accountID); err != nil {
		rt.log().Warn("Model sync after sign-in failed", "accountId", accountID, "error", err)
	}
}

// httpxClientIP is the caller's address, for the rate limiter's fallback key.
func httpxClientIP(r *http.Request) string { return httpx.ClientIP(r, false) }

// errNoCatalog is the failure of a sync with no catalog wired.
var errNoCatalog = errors.New("providers: no catalog is wired")

// billingModeForLogin is the billing a sign-in starts a connection with.
func billingModeForLogin(asked string, allowed ...string) string {
	for _, candidate := range allowed {
		if candidate == asked {
			return asked
		}
	}
	return "plan"
}
