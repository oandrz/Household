-- Ordering is created_at, id everywhere (decision 11: no position column, so
-- insertion order IS the order). The one exception is
-- ListAcceptedAgreementProposals: a version is the k-th ACCEPTANCE, and two
-- proposals created A then B can be accepted B then A.

-- name: ListAgreementSections :many
SELECT id, name, created_at FROM agreement_sections
WHERE household_id = $1 ORDER BY created_at, id;

-- Live rows only. The predicate is here rather than in Go so a growing tail
-- of removed rows is never fetched to be filtered away, and it matches
-- agreements_household_live_idx.
-- name: ListLiveAgreements :many
SELECT id, section_id, body, added_by_proposal_id, created_at FROM agreements
WHERE household_id = $1 AND removed_at IS NULL ORDER BY created_at, id;

-- The three proposal reads select p.* plus the same signed_by aggregate, so
-- sqlc generates three structs with identical fields in identical order --
-- which is what makes toAgreementProposal's conversion at each call site
-- legal. Change one select list and the conversion stops compiling: that is
-- the warning, not an accident.
--
-- GROUP BY p.id is enough because id is the primary key (Postgres then treats
-- every other column of p as functionally dependent), the same rule
-- ListRetroActions documents. The FILTER clause is what keeps an unsigned
-- proposal at '{}' rather than array_agg's default one-element array holding
-- a single NULL, and the ORDER BY inside array_agg makes the signature list
-- stable between reads.
-- name: ListOpenAgreementProposals :many
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.status IN ('pending', 'parked')
GROUP BY p.id ORDER BY p.created_at, p.id;

-- name: ListAcceptedAgreementProposals :many
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.status = 'accepted'
GROUP BY p.id ORDER BY p.resolved_at, p.id;

-- One proposal whatever its status, withdrawn included: the withdraw
-- handler's proposer check costs one query rather than a composed document.
-- name: GetAgreementProposal :one
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.id = $2
GROUP BY p.id;

-- No ON CONFLICT here: the unique index decides a name collision and
-- translate maps it by constraint name (decision 19). A "does this name
-- exist" pre-read is a check-then-write two owners can both pass.
-- name: CreateAgreementSection :one
INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, $2, $3)
RETURNING id, name, created_at;

-- The starter set's insert (decision 17): DO NOTHING, so a second click is a
-- no-op rather than a 409.
-- name: CreateAgreementSectionIfAbsent :exec
INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, $2, $3)
ON CONFLICT (household_id, name) DO NOTHING;

-- The starter set's read-back, run inside the same transaction: it is how the
-- caller proves all four landed rather than two of four.
-- name: ListAgreementSectionsNamed :many
SELECT id, name, created_at FROM agreement_sections
WHERE household_id = $1 AND name = ANY(sqlc.arg(names)::text[])
ORDER BY created_at, id;

-- Sign's step 1, lean because FOR UPDATE cannot sit on the GROUP BY/array_agg
-- read above. It also orders two owners pressing Agree on the SAME proposal
-- at the same instant: the second waits here rather than racing.
-- name: LockAgreementProposal :one
SELECT id, kind, status, section_id, target_agreement_id, body, previous_body
FROM agreement_proposals WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- Sign's step 2, and the lock that matters (decision 12): the proposal lock
-- orders two signatures on one proposal and nothing else, so two proposals
-- against the same agreement never contend on it. All three predicates are IN
-- the WHERE -- under READ COMMITTED a waiter re-evaluates them after the
-- holder commits, so a removal or a rewording by the other proposal returns
-- zero rows here. A bare FOR UPDATE plus a Go-side compare defeats exactly
-- that race. CreateProposal makes the same call at propose time.
-- name: LockAgreementTarget :one
SELECT id, section_id FROM agreements
WHERE household_id = $1 AND id = $2 AND removed_at IS NULL AND body = $3 FOR UPDATE;

-- INSERT ... SELECT is the household scoping: a section in another household
-- matches no row, which is indistinguishable from one that does not exist.
-- name: InsertAgreementProposal :one
INSERT INTO agreement_proposals (household_id, kind, status, section_id, target_agreement_id,
    body, previous_body, note, proposed_by_membership_id, created_at)
SELECT sqlc.arg(household_id), sqlc.arg(kind)::text, 'pending', s.id,
       sqlc.narg(target_agreement_id)::uuid, sqlc.arg(body)::text, sqlc.arg(previous_body)::text,
       sqlc.arg(note)::text, sqlc.arg(proposed_by)::uuid, sqlc.arg(created_at)::timestamptz
FROM agreement_sections s
WHERE s.id = sqlc.arg(section_id) AND s.household_id = sqlc.arg(household_id)
RETURNING id;

-- DO UPDATE, never DO NOTHING: it stores nothing new -- signed_at is left
-- alone, keeping the first stamp (decision 16) -- but still counts a row,
-- which is what lets :execrows tell a double-click (1) from a caller who is
-- not an owner here (0). DO NOTHING would make the two identical.
-- name: SignAgreementProposal :execrows
INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at)
SELECT sqlc.arg(proposal_id), m.id, sqlc.arg(signed_at)::timestamptz
FROM memberships m
WHERE m.id = sqlc.arg(membership_id) AND m.household_id = sqlc.arg(household_id) AND m.role = 'owner'
ON CONFLICT (proposal_id, membership_id) DO UPDATE SET membership_id = excluded.membership_id;

-- name: CountAgreementOwners :one
SELECT count(*) FROM memberships WHERE household_id = $1 AND role = 'owner';

-- Joined through memberships: a departed owner's signature is ignored, never
-- deleted (decision 4).
-- name: CountAgreementOwnerSignatures :one
SELECT count(*) FROM agreement_signatures s JOIN memberships m ON m.id = s.membership_id
WHERE s.proposal_id = $1 AND m.household_id = $2 AND m.role = 'owner';

-- Scoped through agreement_sections: the FK alone only proves the section
-- exists SOMEWHERE, and this is where a repointed proposal is caught.
-- name: InsertAgreementFromProposal :one
INSERT INTO agreements (household_id, section_id, body, added_by_proposal_id, created_at)
SELECT sqlc.arg(household_id), s.id, sqlc.arg(body)::text, sqlc.arg(proposal_id),
       sqlc.arg(created_at)::timestamptz
FROM agreement_sections s
WHERE s.id = sqlc.arg(section_id) AND s.household_id = sqlc.arg(household_id)
RETURNING id;

-- AND removed_at IS NULL, so a row already removed is not stamped twice.
-- name: RemoveAgreement :execrows
UPDATE agreements SET removed_at = sqlc.arg(at)::timestamptz, removed_by_proposal_id = sqlc.arg(proposal_id)
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND removed_at IS NULL;

-- name: AcceptAgreementProposal :execrows
UPDATE agreement_proposals SET status = 'accepted', resolved_at = sqlc.arg(at)::timestamptz
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND status IN ('pending', 'parked');

-- resolved_at stays NULL: parking keeps the proposal OPEN (decision 7). The
-- status condition is in the WHERE, never a service if -- a check-then-write
-- races. Nothing here touches a retro table and there is no foreign key to a
-- retro row: the next retro usually does not exist yet, which is exactly when
-- a couple parks something.
-- name: ParkAgreementProposal :execrows
UPDATE agreement_proposals SET status = 'parked', park_note = sqlc.arg(note)::text
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND status IN ('pending', 'parked');

-- The proposer clause is a BACKSTOP; the handler answers first (decision 22).
-- Its second leg is decision 15: once the proposer is no longer an owner
-- here, any owner may withdraw -- without it a proposal a departed partner
-- left behind could never be removed by anyone.
-- Aliased as ap: sqlc v1.30.0 rejects the unaliased form with "column
-- reference \"household_id\" is ambiguous" once the correlated subquery below
-- reaches the UPDATE target by its bare table name. The alias sidesteps that
-- without changing what the statement does -- confirmed by the Withdraw
-- tests, which exercise both the proposer leg and the departed-owner leg
-- against a real Postgres container.
-- name: WithdrawAgreementProposal :execrows
UPDATE agreement_proposals AS ap SET status = 'withdrawn', resolved_at = sqlc.arg(at)::timestamptz
WHERE ap.household_id = sqlc.arg(household_id) AND ap.id = sqlc.arg(id)
  AND ap.status IN ('pending', 'parked')
  AND (ap.proposed_by_membership_id = sqlc.arg(by)
       OR NOT EXISTS (SELECT 1 FROM memberships m
                      WHERE m.id = ap.proposed_by_membership_id
                        AND m.household_id = sqlc.arg(household_id) AND m.role = 'owner'));
