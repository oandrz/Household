package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ErrSessionRevocationFailed is returned by Update and Remove when the
// membership mutation itself succeeded but the follow-up
// SessionRepository.RevokeAllForUser call failed. It exists so a caller can
// tell "the change did not happen at all" (any other error) apart from "the
// change happened, but the member's prior session(s) may still be live" --
// the one outcome the revocation step exists to prevent, so it must never be
// indistinguishable from ordinary failure.
var ErrSessionRevocationFailed = errors.New("membership was updated but revoking the member's sessions failed")

// MemberDeps mirrors AuthDeps/InviteDeps: every port MemberService needs,
// gathered into one struct so NewMemberService has a single, named argument.
type MemberDeps struct {
	Members  MembershipRepository
	Sessions SessionRepository
}

// MemberService lists and changes a household's members. Every rule about who
// may hold which role or capability, and about a household never losing its
// last owner, lives in internal/domain -- this service's job is to fetch the
// facts domain.ValidateMembershipChange and domain.ValidateMembershipRemoval
// need and act on their verdict, not to re-implement either rule.
type MemberService struct {
	d MemberDeps
}

func NewMemberService(d MemberDeps) *MemberService {
	return &MemberService{d: d}
}

func (s *MemberService) List(ctx context.Context, householdID string) ([]MemberView, error) {
	return s.d.Members.List(ctx, householdID)
}

// Update changes a member's role and/or capabilities. The full membership
// list is loaded first so domain.ValidateMembershipChange can weigh the
// change against the whole household -- the last-owner rule is not a
// property of one membership in isolation. A successful change revokes the
// member's sessions: a capability or role change that stayed effective in an
// already-open tab would defeat the point of granting or revoking it.
func (s *MemberService) Update(ctx context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error {
	views, err := s.d.Members.List(ctx, householdID)
	if err != nil {
		return err
	}
	all := membershipsFrom(views)

	if err := domain.ValidateMembershipChange(all, membershipID, role, caps); err != nil {
		return err
	}

	target, err := findMemberView(views, membershipID)
	if err != nil {
		return err
	}

	if err := s.d.Members.Update(ctx, householdID, membershipID, role, caps); err != nil {
		return err
	}

	// The mutation above is already committed by this point. If the
	// revocation below fails, it is deliberately not rolled back: undoing a
	// completed, valid role/capability change to compensate for a
	// revocation failure would trade a small, bounded window (the member's
	// prior session(s) may stay live a little longer than intended) for a
	// larger one (a write that reports success to the caller but silently
	// reverts itself, which is worse than either outcome alone and would
	// need its own failure handling anyway). See auth.go for the same style
	// of documented, deliberate asymmetry.
	if err := s.d.Sessions.RevokeAllForUser(ctx, target.Membership.UserID); err != nil {
		slog.Error("failed to revoke sessions after a membership update",
			"error", err, "household_id", householdID, "membership_id", membershipID)
		return fmt.Errorf("%w: %v", ErrSessionRevocationFailed, err)
	}
	return nil
}

// Remove deletes a membership, refusing to leave the household without an
// owner (domain.ValidateMembershipRemoval). A successful removal revokes the
// removed member's sessions, exactly as Update does, so a removed member's
// open tab stops working immediately rather than riding out its session TTL.
func (s *MemberService) Remove(ctx context.Context, householdID, membershipID string) error {
	views, err := s.d.Members.List(ctx, householdID)
	if err != nil {
		return err
	}
	all := membershipsFrom(views)

	if err := domain.ValidateMembershipRemoval(all, membershipID); err != nil {
		return err
	}

	target, err := findMemberView(views, membershipID)
	if err != nil {
		return err
	}

	if err := s.d.Members.Delete(ctx, householdID, membershipID); err != nil {
		return err
	}

	// Same deliberate asymmetry as Update above: the deletion is not undone
	// if the revocation that follows it fails. Re-creating the just-deleted
	// membership to compensate would trade a small, bounded window (the
	// removed member's prior session(s) may stay live a little longer than
	// intended) for a larger one (a removal that silently un-happens, which
	// is worse than either outcome alone).
	if err := s.d.Sessions.RevokeAllForUser(ctx, target.Membership.UserID); err != nil {
		slog.Error("failed to revoke sessions after a membership removal",
			"error", err, "household_id", householdID, "membership_id", membershipID)
		return fmt.Errorf("%w: %v", ErrSessionRevocationFailed, err)
	}
	return nil
}

// membershipsFrom projects a member list down to the plain memberships
// domain.ValidateMembershipChange and domain.ValidateMembershipRemoval
// operate on -- both take []domain.Membership, not []MemberView, because the
// last-owner rule cares about role and capabilities, not the joined user.
func membershipsFrom(views []MemberView) []domain.Membership {
	out := make([]domain.Membership, len(views))
	for i, v := range views {
		out[i] = v.Membership
	}
	return out
}

// findMemberView locates the MemberView for a membership ID within an
// already-loaded list. It returns domain.ErrNotFound for a miss, matching
// what ValidateMembershipChange/ValidateMembershipRemoval already return for
// the same target -- by the time this is called, either validation has just
// confirmed the ID exists in the same list, or a caller invoked it directly,
// and either way an absent target is the identical "not found" case.
func findMemberView(views []MemberView, membershipID string) (MemberView, error) {
	for _, v := range views {
		if v.Membership.ID == membershipID {
			return v, nil
		}
	}
	return MemberView{}, domain.ErrNotFound
}
