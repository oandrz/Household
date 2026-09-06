package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// agreementFixture is the two-owner household every write test starts from:
// Alex and Casey (insertTestMembership creates owners), one "Money" section,
// and the repository under test. A test needing a third owner adds one the
// same way -- the signing set is every CURRENT owner (decision 4), so a third
// owner is a third signature.
type agreementFixture struct {
	repo    *postgres.AgreementRepo
	db      *postgres.DB
	h       string
	section string
	alex    string
	casey   string
}

func newAgreementFixture(t *testing.T) agreementFixture {
	t.Helper()
	repo, db, h, section := newAgreementRepo(t)
	return agreementFixture{
		repo:    repo,
		db:      db,
		h:       h,
		section: section,
		alex:    insertTestMembership(t, db, h, "Alex"),
		casey:   insertTestMembership(t, db, h, "Casey"),
	}
}

// propose files one add from Alex, which also records Alex's own signature in
// the same transaction (decision 5).
func (f agreementFixture) propose(t *testing.T, body string) usecase.AgreementProposalRecord {
	t.Helper()
	p, err := f.repo.CreateProposal(context.Background(), usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "add",
		SectionID:              f.section,
		Body:                   body,
		ProposedByMembershipID: f.alex,
		CreatedAt:              at(1),
	})
	if err != nil {
		t.Fatalf("CreateProposal(%q): %v", body, err)
	}
	return p
}

// landAdd proposes and then signs as Casey, which completes a two-owner
// signing set, and returns the one live agreement that produced.
func (f agreementFixture) landAdd(t *testing.T, body string) usecase.AgreementRecord {
	t.Helper()
	ctx := context.Background()
	p := f.propose(t, body)
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	}); err != nil {
		t.Fatalf("Sign(%q): %v", body, err)
	}
	doc, err := f.repo.Document(ctx, f.h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Agreements) != 1 {
		t.Fatalf("%d live agreements after landing %q, want exactly 1", len(doc.Agreements), body)
	}
	return doc.Agreements[0]
}

// countRow reads one integer on the POOL rather than through the repository:
// a count that went through the code under test could be wrong in the same
// direction as the bug it is meant to catch.
func countRow(t *testing.T, db *postgres.DB, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// execSQL runs one raw statement on the pool, for the fixtures and the
// deliberate spoils the repository has no method for.
func execSQL(t *testing.T, db *postgres.DB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec (%s): %v", sql, err)
	}
}

// heldConns is bill_repo_test.go:525-536's inline loop lifted into a function,
// because this file makes the same assertion three times. Polled rather than
// sampled once: pgxpool runs a background health check on a 500ms timer that
// briefly acquires an idle connection, so a single sample can catch an
// unrelated blip, while a LEAKED connection never comes back -- requiring the
// count to reach zero within a second tells the two apart without weakening
// the claim. bill_repo_test.go keeps its own copy: rewiring a passing test in
// an unrelated file is not this task's work.
func heldConns(db *postgres.DB) int32 {
	deadline := time.Now().Add(time.Second)
	for {
		held := db.Pool().Stat().AcquiredConns()
		if held == 0 || time.Now().After(deadline) {
			return held
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The fault has to be on the SECOND write or there is nothing for a partial
// write to leak. A proposer who is not an owner here makes the signature
// INSERT match zero rows, and the proposal written a statement earlier must
// go back with it.
//
// The spec words this as "against a section in another household"; for
// CreateProposal that would fail on InsertAgreementProposal, the FIRST
// statement, leaving nothing written to leak -- so the fault used here is the
// proposer instead. Sign, below, is where the foreign-section fault lands on
// a late statement.
func TestCreateProposalWritesNothingWhenTheProposerIsNotAnOwnerHere(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	stranger := insertTestMembership(t, f.db, insertTestHousehold(t, f.db), "Stranger")

	_, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "add",
		SectionID:              f.section,
		Body:                   "we save 20%",
		ProposedByMembershipID: stranger,
		CreatedAt:              at(1),
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 0 {
		t.Fatalf("%d proposals survived a refused signature, want 0", n)
	}
	if held := heldConns(f.db); held != 0 {
		t.Fatalf("%d connection(s) still checked out -- the transaction was never rolled back", held)
	}
}

// The propose-time half of decision 13. CreateProposal makes the same
// comparison Sign's step 2 makes, in its own transaction against its own row:
// without it a stale proposer files an edit nobody can ever sign, and their
// partner is told THEY are the one out of date.
func TestCreateProposalRefusesAStaleTarget(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	target := f.landAdd(t, "we save 20%")

	_, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "edit",
		TargetAgreementID:      target.ID,
		Body:                   "we save 25%",
		PreviousBody:           "we save 15%", // never the stored wording
		ProposedByMembershipID: f.alex,
		CreatedAt:              at(3),
	})
	if !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("err = %v, want ErrAgreementChanged", err)
	}
	// One proposal in the household: the accepted add that landed the
	// target, and nothing this call wrote.
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d proposals, want 1 -- the refused propose wrote nothing", n)
	}
}

// The two ids arrive from a request body, and uuid() folds an unparseable one
// into the same zero UUID an ABSENT one produces (convert.go's uuidLooksValid
// comment). The guard is per kind, and the two kinds answer differently: an
// unknown section is ErrNotFound, an unknown target is ErrAgreementChanged --
// to this household that agreement is indistinguishable from one that has
// been removed, and both mean the same thing to the caller.
func TestCreateProposalRefusesMalformedIDsPerKind(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)

	if _, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID: f.h, Kind: "add", SectionID: "banana", Body: "we save 20%",
		ProposedByMembershipID: f.alex, CreatedAt: at(1),
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("add with an unparseable section: err = %v, want ErrNotFound", err)
	}
	if _, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID: f.h, Kind: "remove", TargetAgreementID: "banana",
		PreviousBody: "we save 20%", ProposedByMembershipID: f.alex, CreatedAt: at(1),
	}); !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("remove with an unparseable target: err = %v, want ErrAgreementChanged", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 0 {
		t.Fatalf("%d proposals, want 0 -- both refusals happen before the transaction opens", n)
	}
}

// Sign's fault is also on a late write: the apply INSERT is scoped through
// agreement_sections, so a proposal repointed at another household's section
// fails there, after the signature has already been written. Nine of the ten
// pool connections (pool.go:23) are held for the duration, which is what
// proves EVERY statement of Sign runs on the transaction's own connection --
// a pool-backed call inside pgx.BeginFunc would block on a connection nothing
// can release. The VisionRepo.Save hang, asserted rather than hoped for.
func TestSignIsOneTransactionOnItsOwnConnection(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	p := f.propose(t, "we save 20%")

	var foreign string
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO agreement_sections (household_id, name, created_at)
		 VALUES ($1, 'Money', now()) RETURNING id`, insertTestHousehold(t, f.db)).Scan(&foreign); err != nil {
		t.Fatalf("insert foreign section: %v", err)
	}
	execSQL(t, f.db, `UPDATE agreement_proposals SET section_id = $1 WHERE id = $2`, foreign, p.ID)

	var hold []*pgxpool.Conn
	for i := 0; i < 9; i++ {
		c, err := f.db.Pool().Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		hold = append(hold, c)
	}
	signCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := f.repo.Sign(signCtx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	})
	for _, c := range hold {
		c.Release()
	}

	// The positive assertion, not "no timeout": a wrapped deadline that
	// errors.Is does not unwrap would slip past a negative check, and the
	// rollback would leave exactly the counts below either way.
	if !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("err = %v, want ErrAgreementChanged -- a deadline here means a statement reached back to the pool", err)
	}
	sigs := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID)
	agreements := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h)
	if sigs != 1 || agreements != 0 {
		t.Fatalf("signatures = %d (want 1, the proposer's), agreements = %d (want 0)", sigs, agreements)
	}
	if held := heldConns(f.db); held != 0 {
		t.Fatalf("%d connection(s) still checked out, want 0", held)
	}
}

// One subtest per predicate of Sign's step 2 SELECT ... FOR UPDATE, so a
// mutation cannot go red on the wrong one. All three answer
// ErrAgreementChanged: "it vanished" and "someone changed it" are the same
// thing to the caller (decision 13), and ErrNotFound would be false -- the
// proposal WAS found, only its target moved.
func TestSignRefusesAChangedTarget(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, f agreementFixture, targetID string)
	}{
		{"target_in_another_household", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements SET household_id = $2 WHERE id = $1`,
				targetID, insertTestHousehold(t, f.db))
		}},
		{"target_removed", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements
				 SET removed_at = now(), removed_by_proposal_id = added_by_proposal_id
				 WHERE id = $1`, targetID)
		}},
		{"target_reworded", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements SET body = 'we save 30%' WHERE id = $1`, targetID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgreementFixture(t) // its own household: a spoiled row must not leak sideways
			target := f.landAdd(t, "we save 20%")
			edit, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
				HouseholdID:            f.h,
				Kind:                   "edit",
				TargetAgreementID:      target.ID,
				Body:                   "we save 25%",
				PreviousBody:           "we save 20%",
				ProposedByMembershipID: f.alex,
				CreatedAt:              at(3),
			})
			if err != nil {
				t.Fatalf("CreateProposal(edit): %v", err)
			}
			tc.spoil(t, f, target.ID)

			if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
				HouseholdID: f.h, ProposalID: edit.ID, MembershipID: f.casey, At: at(4),
			}); !errors.Is(err, domain.ErrAgreementChanged) {
				t.Fatalf("err = %v, want ErrAgreementChanged", err)
			}
			// The refusal wrote nothing: the proposal is untouched and
			// Casey's signature never landed.
			still, err := f.repo.Proposal(ctx, f.h, edit.ID)
			if err != nil {
				t.Fatalf("Proposal: %v", err)
			}
			if still.Status != "pending" {
				t.Fatalf("status = %q, want pending -- a refused sign applies nothing", still.Status)
			}
			if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, edit.ID); n != 1 {
				t.Fatalf("%d signatures on the refused edit, want 1 -- only the proposer's", n)
			}
		})
	}
}

// Three owners (decision 4, counted live and in-transaction): the second
// signature commits with the status unchanged, a repeat of it keeps the FIRST
// signed_at -- asserted by value, not by a row count -- and only the third
// completes the change, writing exactly one agreements row. The last signer
// pressing Agree twice is refused and writes no second row.
func TestSignCompletesOnlyWhenEveryCurrentOwnerHasSigned(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	drew := insertTestMembership(t, f.db, f.h, "Drew")
	p := f.propose(t, "we save 20%") // Alex's own signature is the first

	second, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	})
	if err != nil {
		t.Fatalf("Sign as Casey: %v", err)
	}
	if second.Status != "pending" || second.ResolvedAt != nil {
		t.Fatalf("status = %q resolvedAt = %v after 2 of 3 signatures, want pending and nil",
			second.Status, second.ResolvedAt)
	}

	// A repeat Agree is idempotent and keeps the first signed_at (decision
	// 16). Read out of the column, because a row count stays 1 whether the
	// upsert left signed_at alone or overwrote it.
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(5),
	}); err != nil {
		t.Fatalf("repeat Sign as Casey: %v", err)
	}
	var signedAt time.Time
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT signed_at FROM agreement_signatures WHERE proposal_id = $1 AND membership_id = $2`,
		p.ID, f.casey).Scan(&signedAt); err != nil {
		t.Fatalf("read signed_at: %v", err)
	}
	if !signedAt.Equal(at(2)) {
		t.Fatalf("signed_at = %s, want the first stamp %s -- DO UPDATE must leave it alone", signedAt, at(2))
	}

	// Not an owner HERE: the signature insert selects through memberships.
	stranger := insertTestMembership(t, f.db, insertTestHousehold(t, f.db), "Stranger")
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: stranger, At: at(6),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}

	done, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: drew, At: at(7),
	})
	if err != nil {
		t.Fatalf("Sign as Drew: %v", err)
	}
	if done.Status != "accepted" || done.ResolvedAt == nil || !done.ResolvedAt.Equal(at(7)) {
		t.Fatalf("status = %q resolvedAt = %v, want accepted at %s", done.Status, done.ResolvedAt, at(7))
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d agreements, want exactly 1", n)
	}

	// The last signer double-clicking Agree: refused, and the agreement row
	// is written once.
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: drew, At: at(8),
	}); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("repeat agree err = %v, want ErrAgreementNotOpen", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d agreements after a repeat agree, want still 1", n)
	}
}

// Three owners on purpose. The mutation this test exists for -- deleting the
// status read that precedes the signature upsert -- is invisible in a
// two-owner fixture: there every non-proposer signature is the completing
// one, so the apply step's own guarded UPDATE errors and the rollback hides
// the extra signature. With three owners the signature is non-completing, so
// without the status read Sign COMMITS a signature on a withdrawn proposal
// and returns no error at all, which is what the count below catches.
func TestSignOnAWithdrawnProposalIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	insertTestMembership(t, f.db, f.h, "Drew")
	p := f.propose(t, "we save 20%")
	if _, err := f.repo.Withdraw(ctx, f.h, p.ID, f.alex, at(3)); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(4),
	}); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("err = %v, want ErrAgreementNotOpen", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID); n != 1 {
		t.Fatalf("%d signatures, want 1 -- only the proposer's; the refused sign wrote nothing", n)
	}
}

// The log read back field by field, never count(*): park keeps the proposal
// open with its note through the write path, re-parking replaces the note,
// and withdraw closes it -- after which park is refused, and an id naming no
// row at all is ErrNotFound. Those last two are diagnoseGuardedUpdate's first
// two legs; its third is the test below.
func TestParkThenWithdrawReadBackFieldByField(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	p := f.propose(t, "we cook on Sundays")

	parked, err := f.repo.Park(ctx, f.h, p.ID, "let us talk on Sunday", at(2))
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if parked.Status != "parked" || parked.ParkNote != "let us talk on Sunday" || parked.ResolvedAt != nil {
		t.Fatalf("park gave status %q note %q resolvedAt %v, want parked / that note / nil -- parking keeps it open",
			parked.Status, parked.ParkNote, parked.ResolvedAt)
	}
	reparked, err := f.repo.Park(ctx, f.h, p.ID, "after the holiday", at(3))
	if err != nil {
		t.Fatalf("Park again: %v", err)
	}
	if reparked.ParkNote != "after the holiday" {
		t.Fatalf("park note = %q, want the second one -- re-parking replaces it", reparked.ParkNote)
	}

	// A parked proposal is still open, so it can still be withdrawn.
	gone, err := f.repo.Withdraw(ctx, f.h, p.ID, f.alex, at(4))
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if gone.Status != "withdrawn" || gone.ResolvedAt == nil || !gone.ResolvedAt.Equal(at(4)) {
		t.Fatalf("withdraw gave status %q resolvedAt %v, want withdrawn at %s", gone.Status, gone.ResolvedAt, at(4))
	}

	if _, err := f.repo.Park(ctx, f.h, p.ID, "one more thought", at(5)); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("park after withdraw err = %v, want ErrAgreementNotOpen", err)
	}
	if _, err := f.repo.Park(ctx, f.h, uuid.NewString(), "", at(5)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("park on an unknown id err = %v, want ErrNotFound", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID); n != 1 {
		t.Fatalf("%d signatures, want 1 -- park and withdraw touch no signature", n)
	}
}

// Decision 15 in both directions -- a comparison with its sides swapped
// passes a one-legged test. Then decision 20: deleting the signer's
// membership ROW directly, not the household whose cascade removes both sides
// and proves nothing, must leave the proposal, its signatures and the
// agreement in place.
func TestWithdrawIsTheProposersUntilTheProposerLeavesAndTheRowsOutliveThem(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	landed := f.landAdd(t, "we save 20%")   // 1 accepted proposal, 2 signatures, 1 agreement
	p := f.propose(t, "we cook on Sundays") // 1 pending proposal, 1 signature

	if _, err := f.repo.Withdraw(ctx, f.h, p.ID, f.casey, at(3)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden -- withdraw is the proposer's while they are still an owner", err)
	}
	execSQL(t, f.db, `DELETE FROM memberships WHERE id = $1`, f.alex)

	gone, err := f.repo.Withdraw(ctx, f.h, p.ID, f.casey, at(4))
	if err != nil {
		t.Fatalf("Withdraw after the proposer left: %v", err)
	}
	if gone.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn -- once the proposer is no longer an owner, any owner may withdraw",
			gone.Status)
	}

	// Neither cascaded nor refused: the two membership columns are log
	// columns and carry no ON DELETE action at all.
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 2 {
		t.Fatalf("%d proposals survived the membership delete, want 2", n)
	}
	if n := countRow(t, f.db,
		`SELECT count(*) FROM agreement_signatures s
		 JOIN agreement_proposals p ON p.id = s.proposal_id
		 WHERE p.household_id = $1`, f.h); n != 3 {
		t.Fatalf("%d signatures survived, want 3 -- two on the accepted add, one on the withdrawn proposal", n)
	}
	if n := countRow(t, f.db,
		`SELECT count(*) FROM agreements WHERE id = $1 AND removed_at IS NULL`, landed.ID); n != 1 {
		t.Fatalf("the agreement Alex proposed is gone (%d live rows), want it still there", n)
	}
}

// Park and Withdraw are both a guarded UPDATE with household_id in the
// WHERE clause, diagnosed on a zero-row match by ONE re-read through
// Proposal -- which is the same household_id-scoped SELECT
// TestProposalHidesAnotherHouseholdsProposal (agreement_repo_test.go) already
// pins for the read half. This is that guarantee proven for the write half
// too, called out explicitly rather than left to inference: a real id,
// looked up under the WRONG household, must come back exactly as ErrNotFound
// comes back for an id that does not exist at all, and it must not touch the
// row it found under the correct household.
func TestParkAndWithdrawRefuseAnotherHouseholdsProposal(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	p := f.propose(t, "we cook on Sundays")
	theirs := insertTestHousehold(t, f.db)

	if _, err := f.repo.Park(ctx, theirs, p.ID, "not yours to park", at(2)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Park under a foreign household id = %v, want ErrNotFound", err)
	}
	if _, err := f.repo.Withdraw(ctx, theirs, p.ID, f.alex, at(3)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Withdraw under a foreign household id = %v, want ErrNotFound", err)
	}

	// Untouched: still pending, no park note, under its REAL household.
	still, err := f.repo.Proposal(ctx, f.h, p.ID)
	if err != nil {
		t.Fatalf("Proposal: %v", err)
	}
	if still.Status != "pending" || still.ParkNote != "" {
		t.Fatalf("status = %q parkNote = %q, want pending and empty -- a foreign-household call wrote nothing",
			still.Status, still.ParkNote)
	}
}
