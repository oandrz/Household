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
