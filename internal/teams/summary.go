package teams

import "context"

// MemberOf is the team the caller belongs to, as the Team page shows it.
type MemberOf struct {
	Owner       *TeamUser `json:"owner"`
	Permissions []string  `json:"permissions"`
	JoinedAt    string    `json:"joinedAt"`
}

// Summary is everything the Team page needs for one account: whose team it is
// in (if any), its own members and invite links, and the permission catalog.
type Summary struct {
	MemberOf    *MemberOf `json:"memberOf"`
	Members     []Member  `json:"members"`
	Invites     []Invite  `json:"invites"`
	Permissions []string  `json:"permissions"`
	Presets     Presets   `json:"presets"`
}

// BuildSummary reads the whole Team page state for one account.
func (s *Service) BuildSummary(ctx context.Context, userID string) (Summary, error) {
	current, err := s.membership(ctx, s.DB.R, userID)
	if err != nil {
		return Summary{}, err
	}
	members, err := s.listMembers(ctx, s.DB.R, userID)
	if err != nil {
		return Summary{}, err
	}
	invites, err := s.ListInvites(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{
		Members:     members,
		Invites:     invites,
		Permissions: append([]string{}, PERMISSIONS...),
		Presets: Presets{
			View: append([]string{}, PermissionPresets.View...),
			Keys: append([]string{}, PermissionPresets.Keys...),
			Full: append([]string{}, PermissionPresets.Full...),
		},
	}
	if current != nil {
		owner, err := s.teamUser(ctx, s.DB.R, current.OwnerUserID)
		if err != nil {
			return Summary{}, err
		}
		summary.MemberOf = &MemberOf{Owner: owner, Permissions: current.Permissions, JoinedAt: current.CreatedAt}
	}
	return summary, nil
}
