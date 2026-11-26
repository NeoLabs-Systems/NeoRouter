package routing

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routing groups (groups.js): the connections and models a name routes over,
// with an optional style, a minimum intelligence, a fallback group and a group
// for subagent work.

var groupNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

// Group is one routing group (the routing_groups.config_json row).
type Group struct {
	ID               string   `json:"id,omitempty"`
	Name             string   `json:"name"`
	Accounts         []string `json:"accounts"`
	Models           []string `json:"models"`
	Profile          *string  `json:"profile"`
	MinIntelligence  *float64 `json:"minIntelligence"`
	Fallback         *string  `json:"fallback"`
	SubagentGroup    *string  `json:"subagentGroup,omitempty"`
	PreferredAccount *string  `json:"preferredAccount,omitempty"`
	AllowOutside     *bool    `json:"allowOutside,omitempty"`
	AllowMetered     *bool    `json:"allowMetered,omitempty"`
}

// GroupInput is a group as the API takes it (groups.schema plus the id on
// update).
type GroupInput struct {
	Name             string   `json:"name"`
	Accounts         []string `json:"accounts"`
	Models           []string `json:"models"`
	Profile          *string  `json:"profile"`
	MinIntelligence  *float64 `json:"minIntelligence"`
	Fallback         *string  `json:"fallback"`
	SubagentGroup    *string  `json:"subagentGroup"`
	PreferredAccount *string  `json:"preferredAccount"`
	AllowOutside     *bool    `json:"allowOutside"`
	AllowMetered     *bool    `json:"allowMetered"`
}

// Groups lists a user's routing groups by name.
func (s *Service) Groups(userID string) []*Group {
	if s.opts.DB == nil {
		return nil
	}
	rows, err := s.opts.DB.R.Query(`SELECT id, config_json FROM routing_groups WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*Group
	for rows.Next() {
		var id, config string
		if err := rows.Scan(&id, &config); err != nil {
			continue
		}
		group := &Group{}
		if err := json.Unmarshal([]byte(config), group); err != nil {
			continue
		}
		group.ID = id
		out = append(out, group)
	}
	return out
}

// ResolveGroup finds a group by name or id.
func (s *Service) ResolveGroup(userID, name string) *Group {
	for _, group := range s.Groups(userID) {
		if group.Name == name || group.ID == name {
			return group
		}
	}
	return nil
}

// RequireGroup is ResolveGroup that fails when the group does not exist.
func (s *Service) RequireGroup(userID, name string) (*Group, error) {
	group := s.ResolveGroup(userID, name)
	if group == nil {
		return nil, httpserver.New(400, "UNKNOWN_ROUTING_GROUP", fmt.Sprintf("Unknown routing group %q.", name))
	}
	return group, nil
}

// SaveGroup creates (id nil) or updates a routing group.
func (s *Service) SaveGroup(userID string, input GroupInput, id *string) (*Group, error) {
	if err := validateGroupInput(input); err != nil {
		return nil, err
	}
	all := s.Groups(userID)
	if id != nil {
		found := false
		for _, group := range all {
			if group.ID == *id {
				found = true
				break
			}
		}
		if !found {
			return nil, httpserver.New(404, "GROUP_NOT_FOUND", "Routing group does not exist.")
		}
	}
	for _, group := range all {
		if group.Name == input.Name && (id == nil || group.ID != *id) {
			return nil, httpserver.New(409, "GROUP_NAME_TAKEN", "Routing group name is already used.")
		}
	}
	config := &Group{
		Name:             input.Name,
		Accounts:         input.Accounts,
		Models:           input.Models,
		Profile:          input.Profile,
		MinIntelligence:  input.MinIntelligence,
		Fallback:         input.Fallback,
		SubagentGroup:    input.SubagentGroup,
		PreferredAccount: input.PreferredAccount,
		AllowOutside:     input.AllowOutside,
		AllowMetered:     input.AllowMetered,
	}
	if config.Accounts == nil {
		config.Accounts = []string{}
	}
	if config.Models == nil {
		config.Models = []string{}
	}
	if config.Profile != nil {
		profile := s.ResolveProfile(userID, *config.Profile)
		if profile == nil {
			return nil, httpserver.New(400, "UNKNOWN_PROFILE", "Routing style does not exist.")
		}
		profileID := profile.ID
		config.Profile = &profileID
	}
	var previous *Group
	if id != nil {
		for _, group := range all {
			if group.ID == *id {
				previous = group
				break
			}
		}
	}
	if previous != nil && previous.Name != config.Name {
		return nil, httpserver.New(400, "GROUP_NAME_IMMUTABLE", "Create a new group to change its routing name.")
	}
	// allowOutside ??= previous ? previous.allowOutside !== false : false
	if config.AllowOutside == nil {
		allow := previous != nil && (previous.AllowOutside == nil || *previous.AllowOutside)
		config.AllowOutside = &allow
	}
	if config.AllowMetered == nil {
		allow := previous != nil && (previous.AllowMetered == nil || *previous.AllowMetered)
		config.AllowMetered = &allow
	}
	if config.PreferredAccount == nil && previous != nil {
		config.PreferredAccount = previous.PreferredAccount
	}
	if config.SubagentGroup == nil && previous != nil {
		config.SubagentGroup = previous.SubagentGroup
	}
	if config.PreferredAccount != nil {
		found := false
		for _, account := range config.Accounts {
			if account == *config.PreferredAccount {
				found = true
				break
			}
		}
		if !found {
			return nil, httpserver.New(400, "UNKNOWN_PREFERRED_ACCOUNT", "Choose a preferred connection from the selected connections.")
		}
	}
	if config.Fallback != nil {
		fallback, err := s.RequireGroup(userID, *config.Fallback)
		if err != nil {
			return nil, err
		}
		config.Fallback = &fallback.Name
	}
	if config.SubagentGroup != nil {
		subagent, err := s.RequireGroup(userID, *config.SubagentGroup)
		if err != nil {
			return nil, err
		}
		config.SubagentGroup = &subagent.Name
	}
	if s.opts.DB != nil {
		for _, account := range config.Accounts {
			var exists int
			if err := s.opts.DB.R.QueryRow(`SELECT 1 FROM provider_accounts WHERE id = ? AND user_id = ?`, account, userID).Scan(&exists); err != nil {
				return nil, httpserver.New(400, "UNKNOWN_ACCOUNT", "Routing group account does not belong to this space.")
			}
		}
	}
	// Fallback groups must not form a cycle.
	seen := map[string]bool{}
	next := config
	for next != nil {
		if seen[next.Name] {
			return nil, httpserver.New(400, "GROUP_CYCLE", "Fallback groups must not form a cycle.")
		}
		seen[next.Name] = true
		if next.Fallback == nil {
			next = nil
			continue
		}
		resolved, err := s.RequireGroup(userID, *next.Fallback)
		if err != nil {
			return nil, err
		}
		if resolved.Name == config.Name {
			next = config
			continue
		}
		next = resolved
	}
	if config.SubagentGroup != nil && *config.SubagentGroup == config.Name {
		return nil, httpserver.New(400, "GROUP_CYCLE", "Choose a different group for subagents.")
	}
	groupID := ""
	if id != nil {
		groupID = *id
	}
	if groupID == "" {
		groupID = uuid.NewString()
	}
	if s.opts.DB != nil {
		if _, err := s.opts.DB.W.Exec(`INSERT INTO routing_groups (id, user_id, name, config_json) VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json`, groupID, userID, config.Name, mustJSON(config)); err != nil {
			return nil, fmt.Errorf("routing: save group: %w", err)
		}
	}
	return s.ResolveGroup(userID, groupID), nil
}

// RemoveGroup deletes a routing group that nothing else uses.
func (s *Service) RemoveGroup(userID, id string) error {
	group, err := s.RequireGroup(userID, id)
	if err != nil {
		return err
	}
	all := s.Groups(userID)
	for _, item := range all {
		if item.Fallback != nil && (*item.Fallback == group.Name || *item.Fallback == group.ID) {
			return httpserver.New(409, "GROUP_IN_USE", "Another group uses this fallback.")
		}
		if item.SubagentGroup != nil && *item.SubagentGroup == group.Name && item.ID != group.ID {
			return httpserver.New(409, "GROUP_IN_USE", "Another group uses this group for subagents.")
		}
	}
	if s.opts.DB == nil {
		return nil
	}
	tx, err := s.opts.DB.W.Begin()
	if err != nil {
		return fmt.Errorf("routing: remove group: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE router_keys SET routing_group = NULL WHERE user_id = ? AND routing_group IN (?, ?)`, userID, group.ID, group.Name); err != nil {
		return fmt.Errorf("routing: remove group: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM routing_groups WHERE id = ? AND user_id = ?`, group.ID, userID); err != nil {
		return fmt.Errorf("routing: remove group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("routing: remove group: %w", err)
	}
	return nil
}

// GroupPlanStatus is one plan account's usage in a group, for the UI.
type GroupPlanStatus struct {
	AccountID string   `json:"accountId"`
	Name      string   `json:"name"`
	UsedShare *float64 `json:"usedShare"`
	ResetAt   *int64   `json:"resetAt"`
}

// GroupStatus is a group as GET /routing/groups shows it: the config plus how
// much of each plan it contains is used.
type GroupStatus struct {
	Group
	Plans []GroupPlanStatus `json:"plans"`
}

// GroupStatuses lists the groups with their plan usage.
func (s *Service) GroupStatuses(userID string, now time.Time) []*GroupStatus {
	var entries []*Entry
	if s.opts.Catalog != nil {
		entries = s.opts.Catalog.ListForUser(userID, CatalogQuery{IncludeUnavailable: true})
	}
	labels := map[string]string{}
	if s.opts.DB != nil {
		rows, err := s.opts.DB.R.Query(`SELECT id, label FROM provider_accounts WHERE user_id = ?`, userID)
		if err == nil {
			for rows.Next() {
				var id, label string
				if rows.Scan(&id, &label) == nil {
					labels[id] = label
				}
			}
			rows.Close()
		}
	}
	var out []*GroupStatus
	for _, group := range s.Groups(userID) {
		accounts := map[string]bool{}
		plans := []GroupPlanStatus{}
		for _, entry := range entries {
			if !entry.IsPlan || !s.Contains(group, entry) || accounts[entry.AccountID] {
				continue
			}
			room := s.planRoomOf(entry, now)
			status := GroupPlanStatus{AccountID: entry.AccountID, Name: labels[entry.AccountID]}
			if status.Name == "" {
				status.Name = entry.ProviderName
			}
			if status.Name == "" {
				status.Name = entry.ProviderSlug
			}
			if room != nil {
				used := 1 - room.Share
				status.UsedShare = &used
				status.ResetAt = room.ResetAt
			}
			accounts[entry.AccountID] = true
			plans = append(plans, status)
		}
		out = append(out, &GroupStatus{Group: *group, Plans: plans})
	}
	return out
}

// Contains is groups.contains: whether a model is in a group. Membership is a
// union: an account includes all its models; model patterns can also include
// models across accounts. Key policy still applies separately.
func (s *Service) Contains(group *Group, entry *Entry) bool {
	if group == nil || entry == nil {
		return false
	}
	for _, account := range group.Accounts {
		if account == entry.AccountID {
			return true
		}
	}
	for _, pattern := range group.Models {
		if GlobToRegExp(pattern).MatchString(entry.ID) {
			return true
		}
	}
	return false
}

// validateGroupInput is groups.schema.
func validateGroupInput(input GroupInput) error {
	if !groupNamePattern.MatchString(input.Name) {
		return httpserver.New(400, "VALIDATION_ERROR", "name: Invalid")
	}
	if len(input.Accounts) > 200 {
		return httpserver.New(400, "VALIDATION_ERROR", "accounts: Array must contain at most 200 element(s)")
	}
	if len(input.Models) > 200 {
		return httpserver.New(400, "VALIDATION_ERROR", "models: Array must contain at most 200 element(s)")
	}
	for _, model := range input.Models {
		if model == "" || len([]rune(model)) > 300 {
			return httpserver.New(400, "VALIDATION_ERROR", "models: String must contain from 1 to 300 character(s)")
		}
	}
	for _, account := range input.Accounts {
		if account == "" {
			return httpserver.New(400, "VALIDATION_ERROR", "accounts: String must contain at least 1 character(s)")
		}
	}
	if input.Profile != nil && len([]rune(*input.Profile)) > 80 {
		return httpserver.New(400, "VALIDATION_ERROR", "profile: String must contain at most 80 character(s)")
	}
	if input.Fallback != nil && len([]rune(*input.Fallback)) > 80 {
		return httpserver.New(400, "VALIDATION_ERROR", "fallback: String must contain at most 80 character(s)")
	}
	if input.MinIntelligence != nil && (*input.MinIntelligence < 0 || *input.MinIntelligence > 1) {
		return httpserver.New(400, "VALIDATION_ERROR", "minIntelligence: Number must be between 0 and 1")
	}
	if len(input.Accounts)+len(input.Models) == 0 {
		return httpserver.New(400, "VALIDATION_ERROR", "Choose at least one account or model.")
	}
	return nil
}

// GroupPolicyResolve is group_policy.resolve: the group a request routes over,
// plus the group it was asked for (the alias). Only the client knows whether
// this is a child request: a handoff happens once, before ranking, and never
// changes the key's permissions.
func (s *Service) GroupPolicyResolve(userID, name, workload string, checkAccess func(*Group) error) (group, alias *Group, err error) {
	if name == "" {
		return nil, nil, nil
	}
	alias, err = s.RequireGroup(userID, name)
	if err != nil {
		return nil, nil, err
	}
	if err := checkAccess(alias); err != nil {
		return nil, nil, err
	}
	if workload != "subagent" || alias.SubagentGroup == nil {
		return alias, alias, nil
	}
	group, err = s.RequireGroup(userID, *alias.SubagentGroup)
	if err != nil {
		return nil, nil, err
	}
	if err := checkAccess(group); err != nil {
		return nil, nil, err
	}
	return group, alias, nil
}

// allowsOutside is group_policy.allowsOutside: a group that does not say
// otherwise lets the router leave it when nothing inside can serve.
func allowsOutside(groups ...*Group) bool {
	for _, group := range groups {
		if group != nil && group.AllowOutside != nil && !*group.AllowOutside {
			return false
		}
	}
	return true
}

// allowsMetered is group_policy.allowsMetered.
func allowsMetered(groups ...*Group) bool {
	for _, group := range groups {
		if group != nil && group.AllowMetered != nil && !*group.AllowMetered {
			return false
		}
	}
	return true
}

// groupPreference is group_policy.preference: an active conversation gets first
// use of its existing endpoint; new conversations start on the preferred
// account if it can serve.
func groupPreference(group *Group, entry *Entry, affinityRef *int64) int {
	if group == nil || group.PreferredAccount == nil {
		return 0
	}
	if affinityRef != nil && entry.Ref == *affinityRef {
		return 0
	}
	if entry.AccountID == *group.PreferredAccount {
		return 1
	}
	return 2
}

// groupReason is group_policy.reason: why the choice was not the preferred
// connection.
func groupReason(group *Group, choice *Candidate, ranked []*Candidate, affinityRef *int64) string {
	if group == nil || group.PreferredAccount == nil {
		return ""
	}
	if affinityRef != nil && choice.Entry.Ref == *affinityRef {
		return "same_connection"
	}
	if choice.Entry.AccountID == *group.PreferredAccount {
		return "preferred_connection"
	}
	var skipped []*Candidate
	for _, item := range ranked {
		if item.Entry.AccountID == *group.PreferredAccount {
			skipped = append(skipped, item)
		}
	}
	if len(skipped) == 0 {
		return ""
	}
	anyAvailable := false
	for _, item := range skipped {
		if item.Rejected == "" {
			anyAvailable = true
			break
		}
	}
	if anyAvailable {
		return ""
	}
	for _, item := range skipped {
		if item.Rejected == "account_busy" || item.Rejected == "slot_overflow" {
			return "preferred_busy"
		}
	}
	for _, item := range skipped {
		if item.Rejected == "plan_used_up" || strings.HasPrefix(item.Rejected, "quota_") {
			return "preferred_allowance"
		}
	}
	return "preferred_unavailable"
}
