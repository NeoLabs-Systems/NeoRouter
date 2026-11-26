package app

import (
	"context"
	"reflect"
	"testing"
)

// optionalCollaborators are the fields the assembled app leaves unset on
// purpose, each with the reason.
var optionalCollaborators = map[string]string{
	"AccountData.Now":     "test clock",
	"Accounts.Now":        "test clock",
	"Auth.Now":            "test clock",
	"Auth.Mailer":         "nil sends over SMTP",
	"Auth.sealerCache":    "filled on first use",
	"Live.opts.Resolve":   "nil uses the system resolver",
	"Local.opts.HFFiles":  "nil asks Hugging Face",
	"Local.opts.Hardware": "nil probes this machine",
	"Pools.clock":         "test clock",
	"Status.Now":          "test clock",
	"Status.Storage":      "nil measures the data directory",
}

// Every domain takes what it needs from others as a func or interface field. A
// field nobody sets fails only when someone reaches that feature, so the
// assembled app is checked as a whole.
func TestAssembledAppSetsEveryCollaborator(t *testing.T) {
	a := openApp(t)
	services := reflect.ValueOf(a.svc)
	for i := range services.NumField() {
		name := services.Type().Field(i).Name
		service := services.Field(i)
		if service.IsNil() {
			t.Errorf("service %s is not built", name)
			continue
		}
		unsetCollaborators(t, name, service.Elem())
	}
}

// unsetCollaborators reports nil func and interface fields of a service, and of
// the options it keeps.
func unsetCollaborators(t *testing.T, path string, value reflect.Value) {
	if value.Kind() != reflect.Struct {
		return
	}
	for i := range value.NumField() {
		field := value.Type().Field(i)
		name := path + "." + field.Name
		switch item := value.Field(i); item.Kind() {
		case reflect.Func, reflect.Interface:
			if _, optional := optionalCollaborators[name]; item.IsNil() && !optional {
				t.Errorf("%s is not set", name)
			}
		case reflect.Struct:
			if field.Name == "opts" || field.Name == "options" || field.Name == "Options" {
				unsetCollaborators(t, name, item)
			}
		}
	}
}

// A pool join issues a router key nobody created by hand; the real key store has
// to accept that.
func TestPoolPartnerKeyIsIssued(t *testing.T) {
	h := &signInApp{App: openApp(t)}
	h.register(t)
	var userID string
	if err := h.DB.R.QueryRow(`SELECT id FROM users`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	keyID, secret, err := h.svc.Pools.RouterKeys.CreatePoolKey(context.Background(), userID, "Pool: partners")
	if err != nil || keyID == "" || secret == "" {
		t.Fatalf("CreatePoolKey = %q, %t, %v", keyID, secret != "", err)
	}
	var createdBy any
	if err := h.DB.R.QueryRow(`SELECT created_by_user_id FROM router_keys WHERE id = ?`, keyID).Scan(&createdBy); err != nil || createdBy != nil {
		t.Fatalf("created_by_user_id = %v, %v", createdBy, err)
	}
}
