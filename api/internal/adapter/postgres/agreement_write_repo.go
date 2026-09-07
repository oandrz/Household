package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Sign records one Agree and, when that completes the signing set, applies
// the change -- all in one transaction, on that transaction's OWN connection.
// Every statement below runs on q, never r.q: a pool-backed call inside
// pgx.BeginFunc takes a second connection while the first is still held, and
// enough concurrent signers then deadlock against pool.go's MaxConns -- the
// hang VisionRepo.Save shipped. It is the only method that writes an
// agreements row, and it returns the proposal as it then stands.
func (r *AgreementRepo) Sign(ctx context.Context, in usecase.AgreementSignatureWrite) (usecase.AgreementProposalRecord, error) {
	var out usecase.AgreementProposalRecord
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		h, id := uuid(in.HouseholdID), uuid(in.ProposalID)

		// 1. Lock the proposal. This is also what orders two owners pressing
		// Agree on this proposal at the same instant: the second waits here,
		// and wakes either to 'accepted' (refused below) or to 'pending',
		// where it completes the change itself.
		p, err := q.LockAgreementProposal(ctx, sqlcgen.LockAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "lock agreement proposal")
		}
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return err // a value no migration allowed: a corrupt row, not a caller's typo
		}
		if !status.IsOpen() {
			return domain.ErrAgreementNotOpen
		}
		kind, err := domain.ParseAgreementProposalKind(p.Kind)
		if err != nil {
			return err
		}

		// 2. On EVERY signing and BEFORE the signature lands -- after step 4
		// a middle signer's agreement would be recorded against wording that
		// has already moved. The only place in Sign that compares
		// previous_body, and the section an edit or a remove applies to is
		// the TARGET's, read here rather than trusted from the proposal row.
		sectionID := p.SectionID
		if kind != domain.ProposalAdd {
			target, err := q.LockAgreementTarget(ctx, sqlcgen.LockAgreementTargetParams{
				HouseholdID: h, ID: p.TargetAgreementID, Body: p.PreviousBody,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrAgreementChanged
			}
			if err != nil {
				return translate(err, "lock agreement target")
			}
			sectionID = target.SectionID
		}

		// 3. The signature. Zero rows means the caller is not an owner here
		// -- the backstop behind requireOwner.
		n, err := q.SignAgreementProposal(ctx, sqlcgen.SignAgreementProposalParams{
			ProposalID:   id,
			MembershipID: uuid(in.MembershipID),
			HouseholdID:  h,
			SignedAt:     timestamptz(in.At),
		})
		if err != nil {
			return translate(err, "sign agreement proposal")
		}
		if n == 0 {
			return domain.ErrForbidden
		}

		// 4. Both counts in this transaction -- a service-level count cannot
		// close the window (decision 4) -- and owners >= MinAgreementOwners,
		// so a household down to one owner cannot finish a change nobody is
		// left to agree with. Not complete: fall through and commit with the
		// status unchanged.
		owners, err := q.CountAgreementOwners(ctx, h)
		if err != nil {
			return translate(err, "count agreement owners")
		}
		signed, err := q.CountAgreementOwnerSignatures(ctx, sqlcgen.CountAgreementOwnerSignaturesParams{
			ProposalID: id, HouseholdID: h,
		})
		if err != nil {
			return translate(err, "count agreement owner signatures")
		}
		if owners >= int64(domain.MinAgreementOwners) && signed == owners {
			if err := applyAgreementChange(ctx, q, h, id, sectionID, kind, p, in.At); err != nil {
				return err
			}
		}

		row, err := q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "read agreement proposal back")
		}
		out, err = toAgreementProposal(row)
		return err
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return out, nil
}

// applyAgreementChange is Sign's steps 5 and 6, run only on the signature
// that completes the set. Every stamp carries AND removed_at IS NULL, is
// :execrows, and n == 0 returns ErrAgreementChanged from inside the
// transaction -- step 2 should already have caught it, so this is the loud
// failure if a future edit loses that lock, rather than five of six writes
// committing.
func applyAgreementChange(ctx context.Context, q *sqlcgen.Queries, h, proposalID, sectionID pgtype.UUID,
	kind domain.AgreementProposalKind, p sqlcgen.LockAgreementProposalRow, at time.Time) error {
	add := func() error {
		_, err := q.InsertAgreementFromProposal(ctx, sqlcgen.InsertAgreementFromProposalParams{
			HouseholdID: h,
			SectionID:   sectionID,
			Body:        p.Body,
			ProposalID:  proposalID,
			CreatedAt:   timestamptz(at),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The section is not this household's any more, which to the
			// caller is the same thing as the target having moved.
			return domain.ErrAgreementChanged
		}
		return translate(err, "insert agreement from proposal")
	}
	remove := func() error {
		n, err := q.RemoveAgreement(ctx, sqlcgen.RemoveAgreementParams{
			At:          timestamptz(at),
			ProposalID:  proposalID,
			HouseholdID: h,
			ID:          p.TargetAgreementID,
		})
		if err != nil {
			return translate(err, "remove agreement")
		}
		if n == 0 {
			return domain.ErrAgreementChanged
		}
		return nil
	}

	// Fail closed: kind came out of a column, so an unrecognised one refuses
	// rather than committing a signature that changed nothing.
	switch kind {
	case domain.ProposalAdd:
		if err := add(); err != nil {
			return err
		}
	case domain.ProposalRemove:
		if err := remove(); err != nil {
			return err
		}
	case domain.ProposalEdit:
		// Both or neither: the old wording is stamped removed and the new one
		// inserted, inside the transaction that is already holding the row.
		if err := remove(); err != nil {
			return err
		}
		if err := add(); err != nil {
			return err
		}
	default:
		return domain.ErrUnknownAgreementProposalKind
	}

	n, err := q.AcceptAgreementProposal(ctx, sqlcgen.AcceptAgreementProposalParams{
		At: timestamptz(at), HouseholdID: h, ID: proposalID,
	})
	if err != nil {
		return translate(err, "accept agreement proposal")
	}
	if n == 0 {
		// Not a sentinel: step 1's lock makes this an invariant, not a state
		// a caller can reach. SetBillNextDue shipped the other way round.
		return fmt.Errorf("accept agreement proposal: %s matched no open row", uuidToString(proposalID))
	}
	return nil
}

// CreateProposal writes the proposal row AND the proposer's implicit
// signature (decision 5) in one transaction: either all of it happens or none
// of it does. A proposal without its proposer's signature would ask both
// owners to be the second signer of a set of one, and no route here could
// repair it. Every statement runs on q, never r.q, for the reason Sign's own
// comment gives.
func (r *AgreementRepo) CreateProposal(ctx context.Context, in usecase.AgreementProposalWrite) (usecase.AgreementProposalRecord, error) {
	// Both ids arrive from a request body, so both are checked BEFORE any SQL:
	// uuid() folds "banana" into the same zero UUID an ABSENT value produces
	// (convert.go's uuidLooksValid comment), and the two must not answer the
	// same way. The check is per kind, because an edit and a remove carry no
	// SectionID at all -- the section is the target's, which is why Validate
	// refuses a caller-supplied one -- and uuidLooksValid("") is false, so an
	// unconditional check would refuse every edit and remove here.
	kind, err := domain.ParseAgreementProposalKind(in.Kind)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if kind == domain.ProposalAdd {
		if !uuidLooksValid(in.SectionID) {
			return usecase.AgreementProposalRecord{}, domain.ErrNotFound
		}
	} else if !uuidLooksValid(in.TargetAgreementID) {
		// Not ErrNotFound: to this household an unparseable target is
		// indistinguishable from one that has been removed, and both mean the
		// same thing to the caller -- what you proposed against is not there.
		return usecase.AgreementProposalRecord{}, domain.ErrAgreementChanged
	}

	var out usecase.AgreementProposalRecord
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		h := uuid(in.HouseholdID)

		// The propose-time half of decision 13, and where an edit or a remove
		// gets its section. The comparison is against in.PreviousBody as
		// stored, never re-trimmed: the service trimmed on the way in, and a
		// second trim would accept wording the proposer never saw.
		sectionID := uuid(in.SectionID)
		if kind != domain.ProposalAdd {
			target, err := q.LockAgreementTarget(ctx, sqlcgen.LockAgreementTargetParams{
				HouseholdID: h, ID: uuid(in.TargetAgreementID), Body: in.PreviousBody,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrAgreementChanged
			}
			if err != nil {
				return translate(err, "lock agreement target")
			}
			sectionID = target.SectionID
		}

		// A section outside this household matches zero rows in the
		// INSERT ... SELECT, which is indistinguishable from one that does
		// not exist: ErrNotFound for both.
		id, err := q.InsertAgreementProposal(ctx, sqlcgen.InsertAgreementProposalParams{
			HouseholdID:       h,
			Kind:              string(kind),
			SectionID:         sectionID,
			TargetAgreementID: nullableUUID(optionalID(in.TargetAgreementID)),
			Body:              in.Body,
			PreviousBody:      in.PreviousBody,
			Note:              in.Note,
			ProposedBy:        uuid(in.ProposedByMembershipID),
			CreatedAt:         timestamptz(in.CreatedAt),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return translate(err, "insert agreement proposal")
		}

		// The second write, and the one the atomicity test faults: zero rows
		// means the proposer is not an owner of this household, and the
		// proposal written a statement ago goes back with it.
		n, err := q.SignAgreementProposal(ctx, sqlcgen.SignAgreementProposalParams{
			ProposalID:   id,
			MembershipID: uuid(in.ProposedByMembershipID),
			HouseholdID:  h,
			SignedAt:     timestamptz(in.CreatedAt),
		})
		if err != nil {
			return translate(err, "sign agreement proposal")
		}
		if n == 0 {
			return domain.ErrForbidden
		}

		row, err := q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "read agreement proposal back")
		}
		out, err = toAgreementProposal(row)
		return err
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return out, nil
}

// Park is Discuss: the proposal stays open and appears in the retro page's
// To-discuss block (decision 7); parking twice replaces the note. One guarded
// UPDATE, no transaction, because it writes one row.
//
// at is the port's and is deliberately unused: parking stamps nothing, since
// resolved_at is what "this is settled" means and a parked proposal is not.
func (r *AgreementRepo) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (usecase.AgreementProposalRecord, error) {
	n, err := r.q.ParkAgreementProposal(ctx, sqlcgen.ParkAgreementProposalParams{
		Note:        note,
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "park agreement proposal")
	}
	if n == 0 {
		return r.diagnoseGuardedUpdate(ctx, householdID, proposalID, "", "park")
	}
	return r.Proposal(ctx, householdID, proposalID)
}

// Withdraw carries the proposer clause in the WHERE, never a service if: a
// check-then-write races. The handler decides and answers first (decision
// 22), so this method never branches on byMembershipID.
func (r *AgreementRepo) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (usecase.AgreementProposalRecord, error) {
	n, err := r.q.WithdrawAgreementProposal(ctx, sqlcgen.WithdrawAgreementProposalParams{
		At:          timestamptz(at),
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
		By:          uuid(byMembershipID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "withdraw agreement proposal")
	}
	if n == 0 {
		return r.diagnoseGuardedUpdate(ctx, householdID, proposalID, byMembershipID, "withdraw")
	}
	return r.Proposal(ctx, householdID, proposalID)
}

// diagnoseGuardedUpdate turns a zero-row guarded UPDATE into the right
// refusal with ONE re-read -- RetroRepo.Update's shape -- and its four legs
// in order: gone, resolved, someone else's while they are still an owner, and
// the re-read's own failure passed through untouched, because "that was
// already settled" is a false claim to make of an unreachable database.
// byMembershipID is "" for park, which has no proposer leg.
func (r *AgreementRepo) diagnoseGuardedUpdate(ctx context.Context, householdID, proposalID, byMembershipID, op string) (usecase.AgreementProposalRecord, error) {
	rec, err := r.Proposal(ctx, householdID, proposalID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return usecase.AgreementProposalRecord{}, domain.ErrNotFound
	case err != nil:
		return usecase.AgreementProposalRecord{}, fmt.Errorf("%s: re-read: %w", op, err)
	}
	status, err := domain.ParseAgreementProposalStatus(rec.Status)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if !status.IsOpen() {
		return usecase.AgreementProposalRecord{}, domain.ErrAgreementNotOpen
	}
	if byMembershipID != "" && rec.ProposedByMembershipID != byMembershipID {
		// Open, someone else's, and that someone is still an owner here --
		// or the SQL's second leg would have matched.
		return usecase.AgreementProposalRecord{}, domain.ErrForbidden
	}
	return usecase.AgreementProposalRecord{}, fmt.Errorf("%s: %s matched no row and no leg explains it", op, proposalID)
}
