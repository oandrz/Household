package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// AgreementRepo keeps the pool alongside the pool-backed *sqlcgen.Queries,
// like VisionRepo, BudgetRepo and GoalRepo: CreateSections here, and
// CreateProposal and Sign in agreement_write_repo.go, each begin their own
// transaction, which a Queries built once at construction time cannot do.
type AgreementRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAgreementRepo(db *DB) *AgreementRepo {
	return &AgreementRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

// Document is the whole screen in one read: sections, live agreements, open
// and accepted proposals. Four queries rather than one join, since the four
// slices sort differently and a join would fan every signature out across
// the product. Withdrawn proposals are excluded in SQL, not by a Go-side
// filter, so nothing can drift from that contract.
func (r *AgreementRepo) Document(ctx context.Context, householdID string) (usecase.AgreementDocument, error) {
	h := uuid(householdID)

	sectionRows, err := r.q.ListAgreementSections(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list agreement sections")
	}
	agreementRows, err := r.q.ListLiveAgreements(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list live agreements")
	}
	openRows, err := r.q.ListOpenAgreementProposals(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list open agreement proposals")
	}
	acceptedRows, err := r.q.ListAcceptedAgreementProposals(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list accepted agreement proposals")
	}

	// make(..., 0, n) on all four: the port's contract is every slice non-nil,
	// because the service hands them to the JSON encoder and null is not [].
	doc := usecase.AgreementDocument{
		Sections:   make([]usecase.AgreementSectionRecord, 0, len(sectionRows)),
		Agreements: make([]usecase.AgreementRecord, 0, len(agreementRows)),
		Open:       make([]usecase.AgreementProposalRecord, 0, len(openRows)),
		Accepted:   make([]usecase.AgreementProposalRecord, 0, len(acceptedRows)),
	}
	for _, s := range sectionRows {
		doc.Sections = append(doc.Sections, usecase.AgreementSectionRecord{
			ID: uuidToString(s.ID), Name: s.Name, CreatedAt: timeOf(s.CreatedAt),
		})
	}
	for _, a := range agreementRows {
		doc.Agreements = append(doc.Agreements, usecase.AgreementRecord{
			ID:                uuidToString(a.ID),
			SectionID:         uuidToString(a.SectionID),
			Body:              a.Body,
			AddedByProposalID: uuidToString(a.AddedByProposalID),
			CreatedAt:         timeOf(a.CreatedAt),
		})
	}
	for _, p := range openRows {
		rec, err := toAgreementProposal(sqlcgen.GetAgreementProposalRow(p))
		if err != nil {
			// Returned unwrapped: a kind or status no migration allows is a
			// corrupt row, and it has to reach the caller as itself rather
			// than as "list open agreement proposals: ...".
			return usecase.AgreementDocument{}, err
		}
		doc.Open = append(doc.Open, rec)
	}
	for _, p := range acceptedRows {
		rec, err := toAgreementProposal(sqlcgen.GetAgreementProposalRow(p))
		if err != nil {
			return usecase.AgreementDocument{}, err
		}
		doc.Accepted = append(doc.Accepted, rec)
	}
	return doc, nil
}

// Proposal answers for one proposal whatever its status, withdrawn
// included -- exactly the row Document's two slices leave out, which the
// withdraw handler needs before it can know whose proposal it is.
func (r *AgreementRepo) Proposal(ctx context.Context, householdID, proposalID string) (usecase.AgreementProposalRecord, error) {
	row, err := r.q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "get agreement proposal")
	}
	return toAgreementProposal(row)
}

// CreateSection is immediate and unsigned: a heading is not a promise. A
// name collision arrives as ErrAgreementSectionNameTaken because translate
// maps agreement_sections_household_id_name_key by name -- the unique index
// decides it, never a check-then-write pre-read two owners could both pass.
func (r *AgreementRepo) CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (usecase.AgreementSectionRecord, error) {
	row, err := r.q.CreateAgreementSection(ctx, sqlcgen.CreateAgreementSectionParams{
		HouseholdID: uuid(householdID),
		Name:        name,
		CreatedAt:   timestamptz(createdAt),
	})
	if err != nil {
		return usecase.AgreementSectionRecord{}, translate(err, "create agreement section")
	}
	return usecase.AgreementSectionRecord{
		ID: uuidToString(row.ID), Name: row.Name, CreatedAt: timeOf(row.CreatedAt),
	}, nil
}

// CreateSections seeds the household's starter set of sections: every name
// in ONE transaction, ON CONFLICT DO NOTHING so a second click is a no-op
// rather than a 409, then read back inside it -- two of four landing would
// leave a household half-seeded with no button left to ask for the rest.
func (r *AgreementRepo) CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]usecase.AgreementSectionRecord, error) {
	out := make([]usecase.AgreementSectionRecord, 0, len(names))
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		for i, name := range names {
			// Stamped strictly apart, never with one shared instant: (created_at,
			// id) is the document's only sort order, so four rows sharing a
			// timestamp would render in random uuid order (00014_agreements.sql
			// explains the deliberately missing DEFAULT now()).
			if err := q.CreateAgreementSectionIfAbsent(ctx, sqlcgen.CreateAgreementSectionIfAbsentParams{
				HouseholdID: uuid(householdID),
				Name:        name,
				CreatedAt:   timestamptz(createdAt.Add(time.Duration(i) * time.Microsecond)),
			}); err != nil {
				return translate(err, "create agreement section if absent")
			}
		}
		rows, err := q.ListAgreementSectionsNamed(ctx, sqlcgen.ListAgreementSectionsNamedParams{
			HouseholdID: uuid(householdID),
			Names:       names,
		})
		if err != nil {
			return translate(err, "list agreement sections named")
		}
		for _, s := range rows {
			out = append(out, usecase.AgreementSectionRecord{
				ID: uuidToString(s.ID), Name: s.Name, CreatedAt: timeOf(s.CreatedAt),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// toAgreementProposal re-parses kind and status rather than casting them: a
// value no migration allowed is a corrupt row, refused here instead of
// reaching a switch upstairs. The cast at its call sites is legal because
// ListOpen…, ListAccepted… and GetAgreementProposal all select p.* plus the
// same signed_by, so sqlc generates three structs with identical fields.
func toAgreementProposal(row sqlcgen.GetAgreementProposalRow) (usecase.AgreementProposalRecord, error) {
	kind, err := domain.ParseAgreementProposalKind(row.Kind)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	status, err := domain.ParseAgreementProposalStatus(row.Status)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return usecase.AgreementProposalRecord{
		ID:                     uuidToString(row.ID),
		Kind:                   string(kind),
		Status:                 string(status),
		SectionID:              uuidToString(row.SectionID),
		TargetAgreementID:      optionalIDToString(row.TargetAgreementID),
		Body:                   row.Body,
		PreviousBody:           row.PreviousBody,
		Note:                   row.Note,
		ParkNote:               row.ParkNote,
		ProposedByMembershipID: uuidToString(row.ProposedByMembershipID),
		CreatedAt:              timeOf(row.CreatedAt),
		ResolvedAt:             timePtrOf(row.ResolvedAt),
		SignedByMembershipIDs:  membershipIDs(row.SignedBy),
	}, nil
}

// membershipIDs is assigneeIDs (retro_action_repo.go) with the one
// difference this port needs: an unsigned proposal gets [], never nil, since
// AwaitingSignature ranges over this list and a nil slice would be one more
// shape for the service to think about for no benefit.
func membershipIDs(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, uuidToString(id))
	}
	return out
}
