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
	// APITokens is revoked wherever Sessions is: a member whose role
	// changed or who was removed must not keep working through a token
	// their browser session no longer has.
	APITokens APITokenRepository
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

// MembershipPatch is a change to one membership where a nil field means
// "keep what the membership has". Both are pointers for the same reason the
// HTTP request's fields are: a zero value cannot tell "left out" apart from
// "set to empty".
type MembershipPatch struct {
	Role         *domain.Role
	Capabilities *domain.Capabilities
}

// Update applies patch to a member's role and/or capabilities and returns the
// membership as written.
//
// Everything that reads the membership's current state happens INSIDE the
// write's own transaction, on the memberships read under the household's
// lock -- never from a read made before it:
//
//   - Omitted fields are filled in from that locked read. Filled in from an
//     earlier read, a capabilities-only change can carry a stale role into
//     the write and silently undo a promotion that committed in between.
//   - domain.ValidateMembershipChange weighs the resolved change against the
//     whole household. The last-owner rule is about the whole list, and two
//     owners demoting each other are each legal alone and illegal together.
//
// The rules stay here; UpdateWithCheck supplies the lock. Role and
// capabilities are validated together, so a role-only patch is checked
// against the member's existing capabilities and a capabilities-only patch
// against their existing role.
//
// A successful change revokes the member's sessions: a capability or role
// change that stayed effective in an already-open tab would defeat the point
// of granting or revoking it. If only that revocation fails, the written
// membership is still returned alongside ErrSessionRevocationFailed.
func (s *MemberService) Update(ctx context.Context, householdID, membershipID string, patch MembershipPatch) (domain.Membership, error) {
	// written is set by the callback from the same locked read it validated,
	// so the response and the revocation below describe exactly the change
	// that was applied.
	var written domain.Membership
	err := s.d.Members.UpdateWithCheck(ctx, householdID, membershipID,
		func(current []domain.Membership) (domain.Role, domain.Capabilities, error) {
			target, err := membershipIn(current, membershipID)
			if err != nil {
				return "", nil, err
			}
			if patch.Role != nil {
				target.Role = *patch.Role
			}
			if patch.Capabilities != nil {
				target.Capabilities = *patch.Capabilities
			}
			if err := domain.ValidateMembershipChange(current, membershipID, target.Role, target.Capabilities); err != nil {
				return "", nil, err
			}
			written = target
			return target.Role, target.Capabilities, nil
		})
	if err != nil {
		return domain.Membership{}, err
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
	if err := s.revokeCredentials(ctx, written.UserID); err != nil {
		slog.Error("failed to revoke credentials after a membership update",
			"error", err, "household_id", householdID, "membership_id", membershipID)
		return written, fmt.Errorf("%w: %v", ErrSessionRevocationFailed, err)
	}
	return written, nil
}

// revokeCredentials is the one place "this person's access is reset" is
// spelled out: sessions and API tokens together, so a later credential
// type is added here and nowhere else. Both are attempted even if the first
// fails; the first error is reported.
func (s *MemberService) revokeCredentials(ctx context.Context, userID string) error {
	sessErr := s.d.Sessions.RevokeAllForUser(ctx, userID)
	tokErr := s.d.APITokens.RevokeAllForUser(ctx, userID)
	if sessErr != nil {
		return sessErr
	}
	return tokErr
}

// Remove deletes a membership, refusing to leave the household without an
// owner (domain.ValidateMembershipRemoval). As in Update, the rule runs inside
// DeleteWithCheck's transaction under the household's lock, so a removal
// cannot race a concurrent demotion or removal of the other owner. A
// successful removal revokes the removed member's sessions, exactly as Update
// does, so a removed member's open tab stops working immediately rather than
// riding out its session TTL.
func (s *MemberService) Remove(ctx context.Context, householdID, membershipID string) error {
	var targetUserID string
	err := s.d.Members.DeleteWithCheck(ctx, householdID, membershipID,
		func(current []domain.Membership) error {
			if err := domain.ValidateMembershipRemoval(current, membershipID); err != nil {
				return err
			}
			target, err := membershipIn(current, membershipID)
			targetUserID = target.UserID
			return err
		})
	if err != nil {
		return err
	}

	// Same deliberate asymmetry as Update above: the deletion is not undone
	// if the revocation that follows it fails. Re-creating the just-deleted
	// membership to compensate would trade a small, bounded window (the
	// removed member's prior session(s) may stay live a little longer than
	// intended) for a larger one (a removal that silently un-happens, which
	// is worse than either outcome alone).
	if err := s.revokeCredentials(ctx, targetUserID); err != nil {
		slog.Error("failed to revoke credentials after a membership removal",
			"error", err, "household_id", householdID, "membership_id", membershipID)
		return fmt.Errorf("%w: %v", ErrSessionRevocationFailed, err)
	}
	return nil
}

// membershipIn finds a membership by ID in the list a locked callback was
// handed. It returns domain.ErrNotFound for a miss, matching what
// ValidateMembershipChange/ValidateMembershipRemoval return for the same
// target, so an unknown ID reads the same whichever of them notices first.
func membershipIn(memberships []domain.Membership, membershipID string) (domain.Membership, error) {
	for _, m := range memberships {
		if m.ID == membershipID {
			return m, nil
		}
	}
	return domain.Membership{}, domain.ErrNotFound
}

// membershipsFrom projects a member list down to the plain memberships the
// domain rules operate on -- they take []domain.Membership, not
// []MemberView, because rules like the last-owner rule care about role and
// capabilities, not the joined user. AgreementService uses it.
func membershipsFrom(views []MemberView) []domain.Membership {
	out := make([]domain.Membership, len(views))
	for i, v := range views {
		out[i] = v.Membership
	}
	return out
}
