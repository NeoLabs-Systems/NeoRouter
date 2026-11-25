package quota

import (
	"encoding/json"
	"testing"
)

func TestQuotasView(t *testing.T) {
	service := testService(t)
	service.defs = definitions{"host": {Limits: Limits{Pool: "account", Account: map[string]float64{RPM: 10}}}}
	service.SetCatalog(catalog{"u/chat": {
		{Ref: 11, ID: "host/one", Name: "One", AccountID: "acct", DefinitionID: "host", ModelID: "one", IsFree: true},
		{Ref: 12, ID: "gone/one", Name: "Gone", AccountID: "missing", DefinitionID: "host", ModelID: "one", IsFree: true},
	}})
	quotas := &Quotas{
		Service:      service,
		Now:          service.Now,
		Capabilities: func() []string { return []string{"chat"} },
		Accounts: func(string) []Account {
			return []Account{{ID: "acct", DefinitionID: "host", JSON: json.RawMessage(`{"id":"acct"}`)}}
		},
		ConcurrencyLimit: func(accountID, definitionID string) *Concurrency {
			if accountID != "acct" || definitionID != "host" {
				t.Fatalf("ConcurrencyLimit(%q, %q)", accountID, definitionID)
			}
			return &Concurrency{Limit: 4, Source: SourceManual}
		},
	}
	view, err := quotas.View("u")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"accounts":[{` +
		`"account":{"id":"acct"},` +
		`"pool":[{"scope":"p:acct","kind":"pool","metric":"rpm","limit":10,"used":0,"remaining":10,"source":"curated","resetAt":null}],` +
		`"models":[{"ref":11,"id":"host/one","name":"One","isFree":true,"windows":[]}],` +
		`"probes":[],` +
		`"concurrency":{"limit":4,"source":"manual"},` +
		`"limits":[{"metric":"rpm","shared":{"limit":10,"source":"curated"},"perModel":null}]}]}`
	if string(encoded) != want {
		t.Fatalf("view =\n%s\nwant\n%s", encoded, want)
	}
}

func TestQuotasViewWithoutCatalog(t *testing.T) {
	service := testService(t)
	quotas := &Quotas{
		Service: service,
		Accounts: func(string) []Account {
			return []Account{{ID: "acct", DefinitionID: "host", JSON: json.RawMessage(`null`)}}
		},
	}
	view, err := quotas.View("u")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"accounts":[{"account":null,"pool":[],"models":[],"probes":[],"concurrency":null,"limits":[]}]}`
	if string(encoded) != want {
		t.Fatalf("view =\n%s\nwant\n%s", encoded, want)
	}
}
