package postgres_test

import (
	"context"
	"testing"
)

// TestAgreementLogColumnsSurviveTheirMembership proves decision 20's deliberate absence of any ON DELETE
// action on agreement_proposals.proposed_by_membership_id and agreement_signatures.membership_id. Deleting
// the HOUSEHOLD would cascade both sides away and prove nothing, so this deletes the membership row directly:
// CASCADE would silently un-sign an accepted agreement, RESTRICT would make removing an owner impossible
// after their first proposal, and the record of who agreed has to outlive the person leaving.
func TestAgreementLogColumnsSurviveTheirMembership(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	pool := db.Pool()
	householdID := insertTestHousehold(t, db)
	membershipID := insertTestMembership(t, db, householdID, "Andreas")

	// created_at and signed_at are NOT NULL with no default (the migration says why), and an accepted
	// proposal must carry resolved_at or agreement_proposals_resolution_matches_status refuses the row.
	var sectionID string
	if err := pool.QueryRow(ctx, `INSERT INTO agreement_sections (household_id, name, created_at)
		 VALUES ($1, 'Money', now()) RETURNING id`, householdID).Scan(&sectionID); err != nil {
		t.Fatalf("insert section: %v", err)
	}
	var proposalID string
	if err := pool.QueryRow(ctx, `INSERT INTO agreement_proposals (household_id, kind, status, section_id,
		 body, proposed_by_membership_id, created_at, resolved_at)
		 VALUES ($1, 'add', 'accepted', $2, 'We review the budget monthly.', $3, now(), now()) RETURNING id`,
		householdID, sectionID, membershipID).Scan(&proposalID); err != nil {
		t.Fatalf("insert proposal: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at)
		 VALUES ($1, $2, now())`, proposalID, membershipID); err != nil {
		t.Fatalf("insert signature: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agreements (household_id, section_id, body,
		 added_by_proposal_id, created_at) VALUES ($1, $2, 'We review the budget monthly.', $3, now())`,
		householdID, sectionID, proposalID); err != nil {
		t.Fatalf("insert agreement: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("deleting the signer's membership must be allowed: %v", err)
	}
	for _, c := range []struct{ what, query string }{
		{"proposal", `SELECT count(*) FROM agreement_proposals WHERE id = $1`},
		{"signature", `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`},
		{"agreement", `SELECT count(*) FROM agreements WHERE added_by_proposal_id = $1`},
	} {
		var n int
		if err := pool.QueryRow(ctx, c.query, proposalID).Scan(&n); err != nil {
			t.Fatalf("count %s rows: %v", c.what, err)
		}
		if n != 1 {
			t.Fatalf("%s rows after deleting the membership = %d, want 1", c.what, n)
		}
	}
}
