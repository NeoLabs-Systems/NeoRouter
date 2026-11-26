package routing

// Groups: what a name routes over.

import "testing"

// TestGroupContainsIsAUnionOfAccountsAndModelPatterns pins groups.contains: an
// account includes every model it serves, and a model pattern includes models
// across accounts, so membership is the union of the two.
func TestGroupContainsIsAUnionOfAccountsAndModelPatterns(t *testing.T) {
	f := newFixture(t)
	own := f.entry("own", "a")
	other := f.entry("other", "b")
	group := &Group{Name: "work", Accounts: []string{"a"}, Models: []string{"b/*"}}
	if !f.svc.Contains(group, own) {
		t.Errorf("%s is not in the group by its account", own.ID)
	}
	if !f.svc.Contains(group, other) {
		t.Errorf("%s is not in the group by its model pattern", other.ID)
	}
	stranger := f.entry("stranger", "c")
	if f.svc.Contains(group, stranger) {
		t.Errorf("%s is in the group", stranger.ID)
	}
	if f.svc.Contains(group, nil) || f.svc.Contains(nil, own) {
		t.Error("a missing group or model counts as a member")
	}
}
