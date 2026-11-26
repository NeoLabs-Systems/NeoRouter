package routerkeys

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the keys API at /api/v1/keys.
func (k *Keys) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/keys", k.guard("keys.view", k.list))
	mux.Handle("POST /api/v1/keys", k.guard("keys.create", k.create))
	mux.Handle("PATCH /api/v1/keys/{id}", k.guard("keys.edit", k.update))
	mux.Handle("POST /api/v1/keys/{id}/revoke", k.guard("keys.edit", k.revoke))
	mux.Handle("DELETE /api/v1/keys/{id}", k.guard("keys.delete", k.remove))
}

func (k *Keys) guard(permission string, handler http.HandlerFunc) http.Handler {
	if k.Require == nil {
		return handler
	}
	return k.Require(permission)(handler)
}

func (k *Keys) space(r *http.Request) (Space, error) {
	if k.Space == nil {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	space, ok := k.Space(r.Context())
	if !ok {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return space, nil
}

type keysBody struct {
	Keys []PublicKey `json:"keys"`
}

type keyBody struct {
	Key PublicKey `json:"key"`
}

type createdBody struct {
	Key    PublicKey `json:"key"`
	Secret string    `json:"secret"`
}

func (k *Keys) list(w http.ResponseWriter, r *http.Request) {
	space, err := k.space(r)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	keys, err := k.List(space.OwnerID)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, keysBody{Keys: keys})
}

func (k *Keys) create(w http.ResponseWriter, r *http.Request) {
	space, err := k.space(r)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	var in Input
	if err := httpserver.ReadJSONManagement(w, r, &in, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := k.validate(&in, true); err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	key, secret, err := k.Create(space.OwnerID, in, space.ActorID, nil)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdBody{Key: *key, Secret: secret})
}

func (k *Keys) update(w http.ResponseWriter, r *http.Request) {
	space, err := k.space(r)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	var in Input
	if err := httpserver.ReadJSONManagement(w, r, &in, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	if err := k.validate(&in, false); err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	key, err := k.Update(space.OwnerID, r.PathValue("id"), in)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, keyBody{Key: *key})
}

func (k *Keys) revoke(w http.ResponseWriter, r *http.Request) {
	space, err := k.space(r)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	key, err := k.Revoke(space.OwnerID, r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, keyBody{Key: *key})
}

func (k *Keys) remove(w http.ResponseWriter, r *http.Request) {
	space, err := k.space(r)
	if err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	if err := k.Remove(space.OwnerID, r.PathValue("id")); err != nil {
		httpserver.WriteError(w, r, k.Log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validationError is a 400 VALIDATION_ERROR with the field that failed.
func validationError(path, message string) error {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", path+": "+message)
}

// validate checks a router key body. create also requires a name, because a
// patch may leave every field out.
func (k *Keys) validate(in *Input, create bool) error {
	if create && !in.Name.Set {
		return validationError("name", "Required")
	}
	if in.Name.Set {
		if in.Name.Value == nil {
			return validationError("name", "Required")
		}
		if err := checkText("name", *in.Name.Value, 1, 80); err != nil {
			return err
		}
	}
	for path, field := range map[string]bool{
		"mode": in.Mode.Set, "budgetPeriod": in.BudgetPeriod.Set, "poolAccess": in.PoolAccess.Set,
		"tokenPeriod": in.TokenPeriod.Set, "priority": in.Priority.Set, "profile": in.Profile.Set,
		"allowedModels": in.AllowedModels.Set,
	} {
		if !field {
			continue
		}
		if nilField(in, path) {
			return validationError(path, "Required")
		}
	}
	if err := checkEnum("mode", in.Mode.Value, ModeFreeOnly, ModePaid, ModeBoth); err != nil {
		return err
	}
	if err := checkEnum("budgetPeriod", in.BudgetPeriod.Value, "total", "monthly", "daily"); err != nil {
		return err
	}
	if err := checkEnum("poolAccess", in.PoolAccess.Value, PoolOwn, PoolAlso); err != nil {
		return err
	}
	if err := checkEnum("tokenPeriod", in.TokenPeriod.Value, "total", "monthly", "daily"); err != nil {
		return err
	}
	if err := checkEnum("priority", in.Priority.Value, k.levels()...); err != nil {
		return err
	}
	if in.BudgetUSD.Value != nil && *in.BudgetUSD.Value < 0 {
		return validationError("budgetUsd", "Number must be greater than or equal to 0")
	}
	if in.MaxPricePerMillion.Value != nil && *in.MaxPricePerMillion.Value < 0 {
		return validationError("maxPricePerMillion", "Number must be greater than or equal to 0")
	}
	if in.ConcurrentLimit.Value != nil && *in.ConcurrentLimit.Value <= 0 {
		return validationError("concurrentLimit", "Number must be greater than 0")
	}
	if in.RPMLimit.Value != nil && *in.RPMLimit.Value <= 0 {
		return validationError("rpmLimit", "Number must be greater than 0")
	}
	if in.TokenLimit.Value != nil && *in.TokenLimit.Value <= 0 {
		return validationError("tokenLimit", "Number must be greater than 0")
	}
	if in.Profile.Value != nil && len(*in.Profile.Value) > 80 {
		return validationError("profile", "String must contain at most 80 character(s)")
	}
	if in.RoutingGroup.Value != nil {
		if err := checkText("routingGroup", *in.RoutingGroup.Value, 1, 80); err != nil {
			return err
		}
	}
	if in.AllowedModels.Value != nil {
		models := *in.AllowedModels.Value
		if len(models) > 200 {
			return validationError("allowedModels", "Array must contain at most 200 element(s)")
		}
		for _, model := range models {
			if err := checkText("allowedModels", model, 1, 200); err != nil {
				return err
			}
		}
	}
	if in.ExpiresAt.Value != nil {
		if _, ok := parseTime(*in.ExpiresAt.Value); !ok {
			return validationError("expiresAt", "Invalid datetime")
		}
	}
	return nil
}

// nilField reports whether a sent field was sent as null, which only the
// nullable ones allow.
func nilField(in *Input, path string) bool {
	switch path {
	case "mode":
		return in.Mode.Value == nil
	case "budgetPeriod":
		return in.BudgetPeriod.Value == nil
	case "poolAccess":
		return in.PoolAccess.Value == nil
	case "tokenPeriod":
		return in.TokenPeriod.Value == nil
	case "priority":
		return in.Priority.Value == nil
	case "profile":
		return in.Profile.Value == nil
	case "allowedModels":
		return in.AllowedModels.Value == nil
	}
	return false
}

func (k *Keys) levels() []string {
	if len(k.Levels) > 0 {
		return k.Levels
	}
	return DefaultLevels
}

func checkText(path, value string, minLength, maxLength int) error {
	if len(value) < minLength {
		return validationError(path, fmt.Sprintf("String must contain at least %d character(s)", minLength))
	}
	if len(value) > maxLength {
		return validationError(path, fmt.Sprintf("String must contain at most %d character(s)", maxLength))
	}
	return nil
}

func checkEnum(path string, value *string, allowed ...string) error {
	if value == nil {
		return nil
	}
	for _, option := range allowed {
		if *value == option {
			return nil
		}
	}
	quoted := make([]string, 0, len(allowed))
	for _, option := range allowed {
		quoted = append(quoted, "'"+option+"'")
	}
	return validationError(path, fmt.Sprintf("Invalid enum value. Expected %s, received '%s'", strings.Join(quoted, " | "), *value))
}
