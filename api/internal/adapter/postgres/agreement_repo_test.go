package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// at gives every agreement fixture a deterministic instant, with only the
// second varying, so an ordering assertion reads as a sequence.
//
// `grep -n 'func at(' internal/adapter/postgres/*_test.go` finds nothing else
// today; package postgres_test is one namespace across thirty files, so check
// before adding any two-letter helper to it.
func at(sec int) time.Time { return time.Date(2026, 9, 5, 9, 0, sec, 0, time.UTC) }

// newAgreementRepo opens a fresh database (one container per test, this
// package's convention), one household, and one "Money" section created
// THROUGH the repository at at(0). It returns the *postgres.DB as well as the
// repository because these tests seed proposals, agreements and signatures
// with raw SQL: the methods that write them are Task 6's.
//
// openTestDB is schema_test.go:1029 and insertTestHousehold is
// schema_test.go:1018; insertTestMembership (transaction_repo_test.go:38)
// returns an OWNER's membership id, which is what every signing assertion
// below depends on.
func newAgreementRepo(t *testing.T) (*postgres.AgreementRepo, *postgres.DB, string, string) {
	t.Helper()
	db := openTestDB(t)
	repo := postgres.NewAgreementRepo(db)
	householdID := insertTestHousehold(t, db)
	section, err := repo.CreateSection(context.Background(), householdID, "Money", at(0))
	if err != nil {
		t.Fatalf("CreateSection: %v", err)
	}
	return repo, db, householdID, section.ID
}

// insertTestProposal seeds one 'add' proposal. agreement_proposals_shape
// requires an add to carry target_agreement_id NULL, body <> '' and
// previous_body = '', and agreement_proposals_resolution_matches_status
// requires resolved_at exactly when the status is accepted or withdrawn --
// hence the *time.Time, which pgx encodes as SQL NULL when it is nil.
func insertTestProposal(t *testing.T, db *postgres.DB, householdID, sectionID, byMembershipID,
	status, body string, createdAt time.Time, resolvedAt *time.Time) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO agreement_proposals (household_id, kind, status, section_id, body,
		     previous_body, note, proposed_by_membership_id, created_at, resolved_at)
		 VALUES ($1, 'add', $2, $3, $4, '', '', $5, $6, $7) RETURNING id`,
		householdID, status, sectionID, body, byMembershipID, createdAt, resolvedAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert %s proposal %q: %v", status, body, err)
	}
	return id
}

// insertTestAgreement seeds one row of the living document. removed_at and
// removed_by_proposal_id move together or agreements_removal_is_whole refuses
// the row -- "a removal is one event, not half of one" -- so the second stamp
// is written as a CASE over the first rather than as a parameter a caller
// could set independently. The proposal that added the row stands in as the
// one that removed it, because nothing here reads which proposal did the
// removing; what is under test is that both columns move together. The
// ::timestamptz cast is what lets Postgres type a NULL parameter inside CASE.
func insertTestAgreement(t *testing.T, db *postgres.DB, householdID, sectionID, proposalID,
	body string, createdAt time.Time, removedAt *time.Time) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO agreements (household_id, section_id, body, added_by_proposal_id,
		     created_at, removed_at, removed_by_proposal_id)
		 VALUES ($1, $2, $3, $4, $5, $6,
		         CASE WHEN $6::timestamptz IS NULL THEN NULL ELSE $4::uuid END)
		 RETURNING id`,
		householdID, sectionID, body, proposalID, createdAt, removedAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert agreement %q: %v", body, err)
	}
	return id
}

func insertTestSignature(t *testing.T, db *postgres.DB, proposalID, membershipID string, signedAt time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at) VALUES ($1, $2, $3)`,
		proposalID, membershipID, signedAt)
	if err != nil {
		t.Fatalf("insert signature: %v", err)
	}
}

// Two orders in one document: Open by created_at then id -- the tie is what
// proves the second key is there at all -- and Accepted by resolved_at,
// because a version is the k-th ACCEPTANCE and B can be accepted before A.
func TestAgreementDocumentOrdersOpenByCreatedAtAndAcceptedByResolvedAt(t *testing.T) {
	repo, db, h, sec := newAgreementRepo(t)
	alex := insertTestMembership(t, db, h, "Alex")
	first := insertTestProposal(t, db, h, sec, alex, "pending", "A", at(1), nil)
	second := insertTestProposal(t, db, h, sec, alex, "pending", "B", at(1), nil)
	third := insertTestProposal(t, db, h, sec, alex, "parked", "C", at(2), nil)
	late, early := at(6), at(5)
	createdFirst := insertTestProposal(t, db, h, sec, alex, "accepted", "D", at(3), &late)
	createdSecond := insertTestProposal(t, db, h, sec, alex, "accepted", "E", at(4), &early)

	doc, err := repo.Document(context.Background(), h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Open) != 3 || len(doc.Accepted) != 2 {
		t.Fatalf("open = %d accepted = %d, want 3 and 2", len(doc.Open), len(doc.Accepted))
	}

	tied := []string{first, second}
	slices.Sort(tied) // uuid byte order is this string's order: fixed-width lowercase hex
	want := append(tied, third)
	got := []string{doc.Open[0].ID, doc.Open[1].ID, doc.Open[2].ID}
	if !slices.Equal(got, want) {
		t.Fatalf("open = %v, want %v (created_at then id)", got, want)
	}
	if doc.Accepted[0].ID != createdSecond || doc.Accepted[1].ID != createdFirst {
		t.Fatalf("accepted = %v, want %s then %s -- ordered by resolved_at, not created_at",
			[]string{doc.Accepted[0].ID, doc.Accepted[1].ID}, createdSecond, createdFirst)
	}
}

// Withdrawn proposals are excluded IN SQL and a removed agreement leaves the
// live document while keeping its row (decision 9) -- but Proposal still
// answers for the withdrawn one, which is the whole reason that method
// exists. The signature assertions are here because this is the only place
// the LEFT JOIN + array_agg is read: without the FILTER clause an unsigned
// proposal comes back holding one NULL rather than nothing.
func TestAgreementDocumentExcludesWithdrawnProposalsAndRemovedAgreements(t *testing.T) {
	ctx := context.Background()
	repo, db, h, sec := newAgreementRepo(t)
	alex := insertTestMembership(t, db, h, "Alex")
	resolved := at(4)
	withdrawn := insertTestProposal(t, db, h, sec, alex, "withdrawn", "we skip date night", at(1), &resolved)
	accepted := insertTestProposal(t, db, h, sec, alex, "accepted", "we save 20%", at(2), &resolved)
	insertTestSignature(t, db, accepted, alex, at(3))
	removedAt := at(5)
	insertTestAgreement(t, db, h, sec, accepted, "we save 20%", at(4), nil)
	insertTestAgreement(t, db, h, sec, accepted, "we skip date night", at(4), &removedAt)

	doc, err := repo.Document(ctx, h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	for _, p := range append(append([]usecase.AgreementProposalRecord{}, doc.Open...), doc.Accepted...) {
		if p.ID == withdrawn {
			t.Fatalf("the withdrawn proposal reached the document as %q -- it must be excluded in SQL", p.Status)
		}
	}
	if len(doc.Agreements) != 1 || doc.Agreements[0].Body != "we save 20%" {
		t.Fatalf("live agreements = %v, want only the one that was never removed", doc.Agreements)
	}
	if len(doc.Accepted) != 1 || !slices.Equal(doc.Accepted[0].SignedByMembershipIDs, []string{alex}) {
		t.Fatalf("accepted signatures = %v, want exactly [%s]", doc.Accepted, alex)
	}

	// The row is still there, and Proposal is what reads it: the withdraw
	// handler's proposer check costs one query rather than a composed
	// document.
	rec, err := repo.Proposal(ctx, h, withdrawn)
	if err != nil {
		t.Fatalf("Proposal(withdrawn): %v", err)
	}
	if rec.Status != "withdrawn" || rec.ResolvedAt == nil || !rec.ResolvedAt.Equal(resolved) {
		t.Fatalf("status = %q resolvedAt = %v, want withdrawn at %s", rec.Status, rec.ResolvedAt, resolved)
	}
	if rec.SignedByMembershipIDs == nil {
		t.Fatalf("SignedByMembershipIDs = nil on an unsigned proposal, want an empty non-nil slice")
	}
}

// Decision 19: the collision is named, not the generic ErrAlreadyExists. The
// second leg goes round Go entirely, pinning the const to the migration's
// real constraint name rather than to what this package believes it is.
func TestCreateSectionTwiceIsAgreementSectionNameTaken(t *testing.T) {
	ctx := context.Background()
	repo, db, h, _ := newAgreementRepo(t) // already created "Money" at at(0)

	if _, err := repo.CreateSection(ctx, h, "Money", at(1)); !errors.Is(err, domain.ErrAgreementSectionNameTaken) {
		t.Fatalf("err = %v, want ErrAgreementSectionNameTaken", err)
	}

	_, err := db.Pool().Exec(ctx,
		`INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, 'Money', now())`, h)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("raw insert err = %v, want a *pgconn.PgError", err)
	}
	if pgErr.ConstraintName != "agreement_sections_household_id_name_key" {
		t.Fatalf("constraint = %q, want agreement_sections_household_id_name_key -- translate matches this string",
			pgErr.ConstraintName)
	}
}

// Decision 17's starter set: four names, one transaction, a second call a
// no-op, and the read-back proving all four landed rather than two of four.
// This household has no proposals or agreements, so it is also where the
// port's "every slice non-nil" is asserted -- the service serialises these
// onto the wire, and Go decodes null and [] identically.
func TestCreateSectionsIsIdempotentAndEverySliceIsNonNil(t *testing.T) {
	ctx := context.Background()
	repo, _, h, _ := newAgreementRepo(t) // "Money" already exists, created at at(0)
	names := []string{"Money", "Conflict", "Home & kids", "Us"}

	firstCall, err := repo.CreateSections(ctx, h, names, at(1))
	if err != nil {
		t.Fatalf("CreateSections: %v", err)
	}
	if len(firstCall) != 4 {
		t.Fatalf("%d sections read back, want 4 -- two of four landing leaves a household half-seeded", len(firstCall))
	}

	secondCall, err := repo.CreateSections(ctx, h, names, at(2))
	if err != nil {
		t.Fatalf("CreateSections again: %v", err)
	}
	firstIDs := make([]string, 0, 4)
	for _, s := range firstCall {
		firstIDs = append(firstIDs, s.ID)
	}
	secondIDs := make([]string, 0, 4)
	for _, s := range secondCall {
		secondIDs = append(secondIDs, s.ID)
	}
	if !slices.Equal(firstIDs, secondIDs) {
		t.Fatalf("second call gave %v, want the same rows %v -- ON CONFLICT DO NOTHING, so a second click is a no-op",
			secondIDs, firstIDs)
	}

	doc, err := repo.Document(ctx, h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Sections) != 4 {
		t.Fatalf("%d sections in the document, want 4 -- a duplicate name must not create a fifth row", len(doc.Sections))
	}
	// Stamped strictly apart and in the order given, so created_at, id is a
	// total order: "Money" keeps at(0) from newAgreementRepo and "Us" is last.
	if doc.Sections[0].Name != "Money" || doc.Sections[3].Name != "Us" {
		t.Fatalf("sections = %v, want Money first and Us last", doc.Sections)
	}
	if doc.Sections == nil || doc.Agreements == nil || doc.Open == nil || doc.Accepted == nil {
		t.Fatalf("a nil slice reached the port: sections=%v agreements=%v open=%v accepted=%v",
			doc.Sections == nil, doc.Agreements == nil, doc.Open == nil, doc.Accepted == nil)
	}
}

// Proposal is the one method in this task that takes an id scoped to
// something other than the household itself (a proposal id, not a
// household id) -- exactly the shape that leaks across households if the
// WHERE clause is ever wrong. TestGetHidesABillFromAnotherHousehold
// (bill_repo_test.go:599) is the same test for BillRepository.Get; this is
// its AgreementRepo counterpart. A row belonging to another household must
// be indistinguishable from one that does not exist, so the answer is
// ErrNotFound, never a different sentinel and never the row itself.
func TestProposalHidesAnotherHouseholdsProposal(t *testing.T) {
	ctx := context.Background()
	repo, db, mine, _ := newAgreementRepo(t)
	theirs := insertTestHousehold(t, db)
	theirSection, err := repo.CreateSection(ctx, theirs, "Money", at(0))
	if err != nil {
		t.Fatalf("CreateSection (theirs): %v", err)
	}
	them := insertTestMembership(t, db, theirs, "Them")
	foreign := insertTestProposal(t, db, theirs, theirSection.ID, them, "pending", "X", at(1), nil)

	_, err = repo.Proposal(ctx, mine, foreign)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Proposal across households = %v, want ErrNotFound (not forbidden, not a row)", err)
	}
}
