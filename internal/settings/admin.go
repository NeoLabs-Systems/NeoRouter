package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the admin endpoints this package owns:
//
//	GET   /api/v1/admin/settings
//	PATCH /api/v1/admin/settings
//
// The rest of the admin API belongs to other domains: /users, /users/{id} and
// /team-audit (auth and teams), /metadata/refresh (catalog), /proxies
// (proxies), /local-models (local models), /update (updates).
//
// These handlers must be registered behind the admin guard (session, space, own
// space, admin role); they do not authenticate on their own.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/settings", s.handleGetAdminSettings)
	mux.HandleFunc("PATCH /api/v1/admin/settings", s.handlePatchAdminSettings)
	// Any other method on the path gets the not-found envelope.
	mux.HandleFunc("/api/v1/admin/settings", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.NotFound(w)
	})
}

// adminSettingsResponse is the body of the admin settings.
type adminSettingsResponse struct {
	RegistrationOpen bool `json:"registrationOpen"`
	Metadata         any  `json:"metadata"`
	PrivacyModel     any  `json:"privacyModel"`
}

func (s *Service) adminSettings(ctx context.Context) (adminSettingsResponse, error) {
	var out adminSettingsResponse
	open, err := s.RegistrationOpen(ctx)
	if err != nil {
		return out, err
	}
	out.RegistrationOpen = open
	if s.MetadataStatus != nil {
		out.Metadata = s.MetadataStatus()
	}
	if s.PrivacyModelStatus != nil {
		out.PrivacyModel = s.PrivacyModelStatus()
	}
	return out, nil
}

func (s *Service) handleGetAdminSettings(w http.ResponseWriter, r *http.Request) {
	body, err := s.adminSettings(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (s *Service) handlePatchAdminSettings(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := httpserver.ReadJSONManagement(w, r, &raw, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	patch, err := parseAdminPatch(raw)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	ctx := r.Context()
	if patch.registrationOpen != nil {
		if err := s.SetGlobal(ctx, KeyRegistrationOpen, *patch.registrationOpen); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
	}
	if patch.artificialAnalysisKey.set {
		if s.SetArtificialAnalysisKey == nil {
			httpserver.WriteError(w, r, s.log(), errors.New("settings: SetArtificialAnalysisKey is not wired"))
			return
		}
		if err := s.SetArtificialAnalysisKey(ctx, patch.artificialAnalysisKey.value); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
	}
	if patch.privacyModel != nil {
		if s.SetPrivacyModelEnabled == nil {
			httpserver.WriteError(w, r, s.log(), errors.New("settings: SetPrivacyModelEnabled is not wired"))
			return
		}
		if err := s.SetPrivacyModelEnabled(ctx, *patch.privacyModel); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
	}
	body, err := s.adminSettings(ctx)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// adminPatch is a validated PATCH body. Every field is optional; the pointer
// fields tell an absent field from a present one.
type adminPatch struct {
	registrationOpen      *bool
	artificialAnalysisKey optionalString
	privacyModel          *bool
}

// optionalString is a nullable field: set separates absent from null.
type optionalString struct {
	set   bool
	value *string
}

// validationDetails holds the form and field errors: both members are always
// present.
type validationDetails struct {
	FormErrors  []string       `json:"formErrors"`
	FieldErrors *orderedObject `json:"fieldErrors"`
}

// parseAdminPatch validates a PATCH body:
//
//	z.object({
//	  registrationOpen: z.boolean().optional(),
//	  artificialAnalysisKey: z.string().max(512).nullable().optional(),
//	  privacyModel: z.boolean().optional(),
//	})
//
// The 400 body is VALIDATION_ERROR, the first issue as "field: message", and
// the flattened issue list as details.
func parseAdminPatch(raw json.RawMessage) (adminPatch, error) {
	var patch adminPatch
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		// A request without a body leaves req.body as {}, which validates.
		return patch, nil
	}
	fieldErrors := newOrderedObject()
	formErrors := []string{}
	firstPath, firstMessage := "", ""
	issue := func(path, message string) {
		if firstMessage == "" {
			firstPath, firstMessage = path, message
		}
		if path == "" {
			formErrors = append(formErrors, message)
			return
		}
		if _, reported := fieldErrors.values[path]; reported {
			return // one issue per field in this schema
		}
		encoded, _ := marshalNoEscape([]string{message})
		fieldErrors.set(path, encoded)
	}
	if trimmed[0] != '{' {
		// body-parser's strict mode already refused scalars, so an array is the
		// only shape that reaches here; the rest is kept for direct callers.
		issue("", "Expected object, received "+jsonTypeName(trimmed))
		return patch, validationError(firstPath, firstMessage, formErrors, fieldErrors)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return patch, fmt.Errorf("settings: read admin settings body: %w", err)
	}

	// The issues are reported in the schema's key order.
	if rawField, present := fields["registrationOpen"]; present {
		if value, ok := readBool(rawField); ok {
			patch.registrationOpen = &value
		} else {
			issue("registrationOpen", "Expected boolean, received "+jsonTypeName(rawField))
		}
	}
	if rawField, present := fields["artificialAnalysisKey"]; present {
		patch.artificialAnalysisKey.set = true
		switch {
		case jsonTypeName(rawField) == "null":
			patch.artificialAnalysisKey.value = nil
		case jsonTypeName(rawField) == "string":
			var value string
			if err := json.Unmarshal(rawField, &value); err != nil {
				return patch, fmt.Errorf("settings: read artificialAnalysisKey: %w", err)
			}
			if utf16Length(value) > 512 {
				issue("artificialAnalysisKey", "String must contain at most 512 character(s)")
				break
			}
			patch.artificialAnalysisKey.value = &value
		default:
			issue("artificialAnalysisKey", "Expected string, received "+jsonTypeName(rawField))
		}
	}
	if rawField, present := fields["privacyModel"]; present {
		if value, ok := readBool(rawField); ok {
			patch.privacyModel = &value
		} else {
			issue("privacyModel", "Expected boolean, received "+jsonTypeName(rawField))
		}
	}
	if firstMessage == "" {
		return patch, nil
	}
	return patch, validationError(firstPath, firstMessage, formErrors, fieldErrors)
}

// readBool reads the JSON literals true and false only.
func readBool(raw json.RawMessage) (bool, bool) {
	switch jsonTypeName(raw) {
	case "boolean":
		return bytes.Equal(bytes.TrimSpace(raw), []byte("true")), true
	default:
		return false, false
	}
}

func validationError(path, message string, formErrors []string, fieldErrors *orderedObject) error {
	if path != "" {
		message = path + ": " + message
	}
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message, validationDetails{
		FormErrors:  formErrors,
		FieldErrors: fieldErrors,
	})
}
