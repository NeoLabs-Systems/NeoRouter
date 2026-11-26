package catalog

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// modelPresetsKey is the per-user setting holding the saved model selections.
const modelPresetsKey = "model_presets"

// Preset is one saved model selection ("DeepSeek + Qwen Max") that every model
// selector can apply in one tap.
type Preset struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

// MountPresets registers the model preset routes.
func (rt *Routes) MountPresets(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/model-presets", http.HandlerFunc(rt.handlePresetList))
	mux.Handle("PUT /api/v1/model-presets", http.HandlerFunc(rt.handlePresetSave))
}

func (rt *Routes) handlePresetList(w http.ResponseWriter, r *http.Request) {
	presets := []Preset{}
	if rt.Svc.Settings != nil {
		value, ok, err := rt.Svc.Settings.UserSetting(rt.userID(r), modelPresetsKey)
		if err != nil {
			rt.fail(w, r, err)
			return
		}
		if ok && len(value) > 0 {
			var stored []Preset
			if err := json.Unmarshal(value, &stored); err == nil {
				presets = stored
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"presets": presets})
}

func (rt *Routes) handlePresetSave(w http.ResponseWriter, r *http.Request) {
	body, err := readObject(w, r)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	presets, err := validatePresets(body)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	if rt.Svc.Settings != nil {
		if err := rt.Svc.Settings.SetUserSetting(rt.userID(r), modelPresetsKey, presets); err != nil {
			rt.fail(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"presets": presets})
}

// userID returns the caller's own user id (presets are personal, whatever
// space is open).
func (rt *Routes) userID(r *http.Request) string {
	if rt.UserID == nil {
		return ""
	}
	return rt.UserID(r)
}

// validatePresets validates a PUT /api/v1/model-presets body the way the zod
// schema in routes/model_presets.js does, including its wording.
func validatePresets(body map[string]any) ([]Preset, error) {
	raw, present := body["presets"]
	if !present {
		return nil, invalid(validationIssue{"presets", "Required"})
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, invalid(validationIssue{"presets", "Expected array, received " + typeName(raw)})
	}
	if len(items) > 50 {
		return nil, invalid(validationIssue{"presets", "Array must contain at most 50 element(s)"})
	}
	presets := make([]Preset, 0, len(items))
	for i, item := range items {
		path := "presets." + strconv.Itoa(i)
		object, ok := item.(map[string]any)
		if !ok {
			return nil, invalid(validationIssue{path, "Expected object, received " + typeName(item)})
		}
		id, err := presetString(object, "id", path+".id", 1, 64, false)
		if err != nil {
			return nil, err
		}
		name, err := presetString(object, "name", path+".name", 1, 60, true)
		if err != nil {
			return nil, err
		}
		preset := Preset{ID: id, Name: name, Models: []string{}}
		modelsRaw, present := object["models"]
		if !present {
			return nil, invalid(validationIssue{path + ".models", "Required"})
		}
		models, ok := modelsRaw.([]any)
		if !ok {
			return nil, invalid(validationIssue{path + ".models", "Expected array, received " + typeName(modelsRaw)})
		}
		if len(models) < 1 {
			return nil, invalid(validationIssue{path + ".models", "Array must contain at least 1 element(s)"})
		}
		if len(models) > 500 {
			return nil, invalid(validationIssue{path + ".models", "Array must contain at most 500 element(s)"})
		}
		for j, model := range models {
			text, ok := model.(string)
			if !ok {
				return nil, invalid(validationIssue{path + ".models." + strconv.Itoa(j), "Expected string, received " + typeName(model)})
			}
			if text == "" {
				return nil, invalid(validationIssue{path + ".models." + strconv.Itoa(j), "String must contain at least 1 character(s)"})
			}
			if len(text) > 300 {
				return nil, invalid(validationIssue{path + ".models." + strconv.Itoa(j), "String must contain at most 300 character(s)"})
			}
			preset.Models = append(preset.Models, text)
		}
		presets = append(presets, preset)
	}
	return presets, nil
}

// presetString reads one required string field of a preset.
func presetString(object map[string]any, key, path string, min, max int, trim bool) (string, error) {
	raw, present := object[key]
	if !present {
		return "", invalid(validationIssue{path, "Required"})
	}
	text, ok := raw.(string)
	if !ok {
		return "", invalid(validationIssue{path, "Expected string, received " + typeName(raw)})
	}
	if trim {
		text = strings.Trim(text, " \t\n\r\v\f\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
	}
	if len(text) < min {
		return "", invalid(validationIssue{path, "String must contain at least " + strconv.Itoa(min) + " character(s)"})
	}
	if len(text) > max {
		return "", invalid(validationIssue{path, "String must contain at most " + strconv.Itoa(max) + " character(s)"})
	}
	return text, nil
}
