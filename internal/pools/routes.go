package pools

// The pool page: start or join pools, invite, choose what each pool shares, pause,
// leave (routes/pools.js). Pools belong to a person's own space; team members of
// that space may look (to know whether pool keys are possible) but not change
// anything.

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// Routes registers the pool API at /api/v1/pools.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/pools", s.poolSummary)
	mux.HandleFunc("POST /api/v1/pools", s.guard(s.poolCreate))
	mux.HandleFunc("POST /api/v1/pools/preview", s.guard(s.poolPreview))
	mux.HandleFunc("POST /api/v1/pools/join", s.guard(s.poolJoin))
	mux.HandleFunc("PUT /api/v1/pools/privacy", s.guard(s.poolPrivacy))
	mux.HandleFunc("PUT /api/v1/pools/partners/{linkId}/paused", s.guard(s.poolPartnerPaused))
	mux.HandleFunc("POST /api/v1/pools/{poolId}/invites", s.guard(s.poolCreateInvite))
	mux.HandleFunc("DELETE /api/v1/pools/{poolId}/invites/{id}", s.guard(s.poolRevokeInvite))
	mux.HandleFunc("PUT /api/v1/pools/{poolId}/share", s.guard(s.poolUpdateShare))
	mux.HandleFunc("PUT /api/v1/pools/{poolId}/accounts/{id}/share", s.guard(s.poolAccountShare))
	mux.HandleFunc("GET /api/v1/pools/{poolId}/activity", s.guard(s.poolActivityHandler))
	mux.HandleFunc("PUT /api/v1/pools/{poolId}/paused", s.guard(s.poolPaused))
	mux.HandleFunc("PUT /api/v1/pools/{poolId}/relay", s.guard(s.poolRelay))
	mux.HandleFunc("DELETE /api/v1/pools/{poolId}/members/{memberId}", s.guard(s.poolRemoveMember))
	mux.HandleFunc("POST /api/v1/pools/{poolId}/leave", s.guard(s.poolLeave))
	mux.HandleFunc("POST /api/v1/pools/{poolId}/sync", s.guard(s.poolSync))
}

// guard applies the own-space rule: a team member may look but not change.
func (s *Service) guard(next http.HandlerFunc) http.HandlerFunc {
	guarded := teams.RequireOwnSpace(next)
	return func(w http.ResponseWriter, r *http.Request) { guarded.ServeHTTP(w, r) }
}

// space is whose pools a request works on.
func (s *Service) space(r *http.Request) (teams.Space, *HttpError) {
	space, ok := teams.SpaceFromContext(r.Context())
	if !ok || space == nil {
		return teams.Space{}, fail(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return *space, nil
}

// owner is the person whose pools a request works on.
func (s *Service) owner(r *http.Request) (string, *HttpError) {
	space, err := s.space(r)
	if err != nil {
		return "", err
	}
	return space.OwnerID, nil
}

// invalid is the 400 a body that does not match its schema answers with.
func invalid(message string) *HttpError {
	return fail(http.StatusBadRequest, "VALIDATION_ERROR", message)
}

// readBody decodes one management request body.
func (s *Service) readBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	body := map[string]any{}
	if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
		return nil, false
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, true
}

// nameField validates a name the way the zod schema does: trimmed, 1 to 60
// characters.
func nameField(body map[string]any, field string) (string, *HttpError) {
	raw, present := body[field]
	if !present {
		return "", invalid(field + ": Required")
	}
	text, ok := raw.(string)
	if !ok {
		return "", invalid(field + ": Expected string, received " + typeName(raw))
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 1 {
		return "", invalid(field + ": String must contain at least 1 character(s)")
	}
	if len([]rune(trimmed)) > 60 {
		return "", invalid(field + ": String must contain at most 60 character(s)")
	}
	return trimmed, nil
}

// accountIDsField validates a list of connection ids.
func accountIDsField(body map[string]any, field string, required bool) ([]string, bool, *HttpError) {
	raw, present := body[field]
	if !present {
		if required {
			return nil, false, invalid(field + ": Required")
		}
		return nil, false, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, false, invalid(field + ": Expected array, received " + typeName(raw))
	}
	if len(list) > 200 {
		return nil, false, invalid(field + ": Array must contain at most 200 element(s)")
	}
	ids := []string{}
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			return nil, false, invalid(field + ": Expected string, received " + typeName(item))
		}
		if len(text) > 64 {
			return nil, false, invalid(field + ": String must contain at most 64 character(s)")
		}
		ids = append(ids, text)
	}
	return ids, true, nil
}

// boolField validates a true/false field.
func boolField(body map[string]any, field string) (bool, *HttpError) {
	raw, present := body[field]
	if !present {
		return false, invalid(field + ": Required")
	}
	value, ok := raw.(bool)
	if !ok {
		return false, invalid(field + ": Expected boolean, received " + typeName(raw))
	}
	return value, nil
}

// typeName names a decoded JSON value for a validation message.
func typeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// boundedField validates one fixed share cap: a whole number inside its bounds,
// or null for the automatic share.
func boundedField(body map[string]any, field string, integer bool) (float64, bool, *HttpError) {
	raw, present := body[field]
	if !present {
		return 0, false, nil
	}
	if raw == nil {
		return 0, true, nil
	}
	number, ok := raw.(float64)
	if !ok {
		return 0, false, invalid(field + ": Expected number, received " + typeName(raw))
	}
	if integer && number != math.Trunc(number) {
		return 0, false, invalid(field + ": Expected integer, received float")
	}
	min, max, _ := ShareBounds(field)
	if number < min {
		return 0, false, invalid(field + ": Number must be greater than or equal to " + numberText(min))
	}
	if number > max {
		return 0, false, invalid(field + ": Number must be less than or equal to " + numberText(max))
	}
	return number, true, nil
}

// shareKindsField validates which kinds each connection shares.
func shareKindsField(body map[string]any) (map[string][]string, *HttpError) {
	raw, present := body["shareKinds"]
	if !present {
		return nil, nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, invalid("shareKinds: Expected object, received " + typeName(raw))
	}
	result := map[string][]string{}
	for accountID, entry := range object {
		list, ok := entry.([]any)
		if !ok {
			return nil, invalid("shareKinds." + accountID + ": Expected array, received " + typeName(entry))
		}
		if len(list) > 3 {
			return nil, invalid("shareKinds." + accountID + ": Array must contain at most 3 element(s)")
		}
		kinds := []string{}
		for _, item := range list {
			text, ok := item.(string)
			if !ok || !containsString(ShareKinds, text) {
				return nil, invalid("shareKinds." + accountID + ": Invalid enum value. Expected 'free' | 'plan' | 'paid'")
			}
			kinds = append(kinds, text)
		}
		result[accountID] = kinds
	}
	return result, nil
}

// strictKeys refuses the fields a .strict() schema does not know.
func strictKeys(body map[string]any, allowed ...string) *HttpError {
	for key := range body {
		if !containsString(allowed, key) {
			return invalid("Unrecognized key(s) in object: '" + key + "'")
		}
	}
	return nil
}

// writeError answers one handler failure.
func (s *Service) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, s.log(), err)
}

// poolSummary is GET /api/v1/pools.
func (s *Service) poolSummary(w http.ResponseWriter, r *http.Request) {
	space, spaceErr := s.space(r)
	if spaceErr != nil {
		s.writeError(w, r, spaceErr)
		return
	}
	summary, err := s.summary(r.Context(), space.OwnerID, space.IsOwner)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolCreate is POST /api/v1/pools.
func (s *Service) poolCreate(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	name, nameErr := nameField(body, "name")
	if nameErr != nil {
		s.writeError(w, r, nameErr)
		return
	}
	selfName, selfErr := nameField(body, "selfName")
	if selfErr != nil {
		s.writeError(w, r, selfErr)
		return
	}
	shared, _, sharedErr := accountIDsField(body, "sharedAccountIds", false)
	if sharedErr != nil {
		s.writeError(w, r, sharedErr)
		return
	}
	created, err := s.create(r.Context(), owner, poolCreate{
		Name: name, SelfName: selfName, SelfURL: s.publicOrigin(r), SharedAccountIDs: shared,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

// linkField validates an invite link.
func linkField(body map[string]any) (string, *HttpError) {
	raw, present := body["link"]
	if !present {
		return "", invalid("link: Required")
	}
	text, ok := raw.(string)
	if !ok {
		return "", invalid("link: Expected string, received " + typeName(raw))
	}
	if len(text) < 1 {
		return "", invalid("link: String must contain at least 1 character(s)")
	}
	if len(text) > 2048 {
		return "", invalid("link: String must contain at most 2048 character(s)")
	}
	return text, nil
}

// poolPreview is POST /api/v1/pools/preview.
func (s *Service) poolPreview(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	link, linkErr := linkField(body)
	if linkErr != nil {
		s.writeError(w, r, linkErr)
		return
	}
	preview, err := s.preview(r.Context(), owner, link)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

// poolJoin is POST /api/v1/pools/join.
func (s *Service) poolJoin(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	link, linkErr := linkField(body)
	if linkErr != nil {
		s.writeError(w, r, linkErr)
		return
	}
	selfName, nameErr := nameField(body, "selfName")
	if nameErr != nil {
		s.writeError(w, r, nameErr)
		return
	}
	shared, _, sharedErr := accountIDsField(body, "sharedAccountIds", false)
	if sharedErr != nil {
		s.writeError(w, r, sharedErr)
		return
	}
	joined, err := s.join(r.Context(), owner, poolJoin{
		Link: link, SelfName: selfName, SelfURL: s.publicOrigin(r), SharedAccountIDs: shared,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, joined)
}

// poolPrivacy is PUT /api/v1/pools/privacy: keep very sensitive requests off
// pools, for all pools.
func (s *Service) poolPrivacy(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	if strictErr := strictKeys(body, "keepSensitivePrivate"); strictErr != nil {
		s.writeError(w, r, strictErr)
		return
	}
	value, boolErr := boolField(body, "keepSensitivePrivate")
	if boolErr != nil {
		s.writeError(w, r, boolErr)
		return
	}
	settings, err := s.SavePoolPrivacy(r.Context(), owner, PoolPrivacyInput{KeepSensitivePrivate: &value})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"privacy": settings})
}

// poolPartnerPaused is PUT /api/v1/pools/partners/{linkId}/paused.
func (s *Service) poolPartnerPaused(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	paused, boolErr := boolField(body, "paused")
	if boolErr != nil {
		s.writeError(w, r, boolErr)
		return
	}
	summary, err := s.setPartnerPaused(r.Context(), owner, r.PathValue("linkId"), paused)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolCreateInvite is POST /api/v1/pools/{poolId}/invites.
func (s *Service) poolCreateInvite(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	invite, err := s.createInvite(r.Context(), owner, r.PathValue("poolId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, invite)
}

// poolRevokeInvite is DELETE /api/v1/pools/{poolId}/invites/{id}.
func (s *Service) poolRevokeInvite(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	if err := s.revokeInvite(r.Context(), owner, r.PathValue("poolId"), r.PathValue("id")); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// poolUpdateShare is PUT /api/v1/pools/{poolId}/share.
func (s *Service) poolUpdateShare(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	if strictErr := strictKeys(body, shareFields[0], shareFields[1], shareFields[2], shareFields[3],
		"sharedAccountIds", "shareKinds"); strictErr != nil {
		s.writeError(w, r, strictErr)
		return
	}
	input := map[string]any{}
	for index, field := range shareFields {
		value, present, fieldErr := boundedField(body, field, index < 3)
		if fieldErr != nil {
			s.writeError(w, r, fieldErr)
			return
		}
		if present {
			if _, isNull := body[field]; isNull && body[field] == nil {
				input[field] = nil
				continue
			}
			input[field] = value
		}
	}
	if shared, present, sharedErr := accountIDsField(body, "sharedAccountIds", false); sharedErr != nil {
		s.writeError(w, r, sharedErr)
		return
	} else if present {
		list := make([]any, 0, len(shared))
		for _, id := range shared {
			list = append(list, id)
		}
		input["sharedAccountIds"] = list
	}
	kinds, kindsErr := shareKindsField(body)
	if kindsErr != nil {
		s.writeError(w, r, kindsErr)
		return
	}
	if kinds != nil {
		object := map[string]any{}
		for accountID, list := range kinds {
			items := make([]any, 0, len(list))
			for _, kind := range list {
				items = append(items, kind)
			}
			object[accountID] = items
		}
		input["shareKinds"] = object
	}
	summary, err := s.updateShare(r.Context(), owner, r.PathValue("poolId"), input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolAccountShare is PUT /api/v1/pools/{poolId}/accounts/{id}/share.
func (s *Service) poolAccountShare(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	change := shareChange{}
	if raw, present := body["shared"]; present {
		value, isBool := raw.(bool)
		if !isBool {
			s.writeError(w, r, invalid("shared: Expected boolean, received "+typeName(raw)))
			return
		}
		change.Shared = value
		change.SharedSet = true
	}
	if raw, present := body["kinds"]; present {
		list, isList := raw.([]any)
		if !isList {
			s.writeError(w, r, invalid("kinds: Expected array, received "+typeName(raw)))
			return
		}
		if len(list) > 3 {
			s.writeError(w, r, invalid("kinds: Array must contain at most 3 element(s)"))
			return
		}
		kinds := []string{}
		for _, item := range list {
			text, isText := item.(string)
			if !isText || !containsString(ShareKinds, text) {
				s.writeError(w, r, invalid("kinds: Invalid enum value. Expected 'free' | 'plan' | 'paid'"))
				return
			}
			kinds = append(kinds, text)
		}
		change.Kinds = kinds
		change.KindsSet = true
	}
	if !change.SharedSet && !change.KindsSet {
		s.writeError(w, r, invalid("Invalid input"))
		return
	}
	if !change.SharedSet {
		change.Shared = true
	}
	summary, err := s.setAccountShared(r.Context(), owner, r.PathValue("poolId"), r.PathValue("id"), change)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolActivityHandler is GET /api/v1/pools/{poolId}/activity.
func (s *Service) poolActivityHandler(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	poolID := r.PathValue("poolId")
	if _, err := s.requirePool(r.Context(), owner, poolID); err != nil {
		s.writeError(w, r, err)
		return
	}
	days := 0
	if raw := r.URL.Query().Get("days"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			days = int(parsed)
		}
	}
	activity, err := s.PoolActivityOf(r.Context(), owner, poolID, days, s.nowMS())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, activity)
}

// poolPaused is PUT /api/v1/pools/{poolId}/paused: the quick switch.
func (s *Service) poolPaused(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	paused, boolErr := boolField(body, "paused")
	if boolErr != nil {
		s.writeError(w, r, boolErr)
		return
	}
	summary, err := s.setPoolPaused(r.Context(), owner, r.PathValue("poolId"), paused)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolRelay is PUT /api/v1/pools/{poolId}/relay: the host's switch.
func (s *Service) poolRelay(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	enabled, boolErr := boolField(body, "enabled")
	if boolErr != nil {
		s.writeError(w, r, boolErr)
		return
	}
	summary, err := s.setRelayEnabled(r.Context(), owner, r.PathValue("poolId"), enabled)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolRemoveMember is DELETE /api/v1/pools/{poolId}/members/{memberId}.
func (s *Service) poolRemoveMember(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	if err := s.removeMember(r.Context(), owner, r.PathValue("poolId"), r.PathValue("memberId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	summary, err := s.summary(r.Context(), owner, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolLeave is POST /api/v1/pools/{poolId}/leave.
func (s *Service) poolLeave(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	summary, err := s.leave(r.Context(), owner, r.PathValue("poolId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// poolSync is POST /api/v1/pools/{poolId}/sync.
func (s *Service) poolSync(w http.ResponseWriter, r *http.Request) {
	owner, ownerErr := s.owner(r)
	if ownerErr != nil {
		s.writeError(w, r, ownerErr)
		return
	}
	if err := s.sync(r.Context(), owner, r.PathValue("poolId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	summary, err := s.summary(r.Context(), owner, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}
