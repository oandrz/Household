# Hearth — Agreements

Spec for slice 3's last feature: the Marriage → Agreements screen, its three
modals, the propose → sign lifecycle behind every change to the document, and
the read-only "To discuss" block this feature adds to the Retros page. Written
2026-09-05 after a brainstorming session; the decisions below record what was
chosen and why, in the order they were made.

The design source is the Agreements section of
`design/Household Dashboard.dc.html` — the "Our agreements" screen and its empty
state, the pending-change card, the `modalPropose` Propose-a-change modal with
its three change types, the `modalSection` New-section modal and the
`modalVersions` Version-history modal — plus the flow map's entry
`4d Marriage · Agreements`: "Versioned living document grouped by Money /
Conflict / Home & kids / Us, with a propose-a-change flow."

**This is the third and last of the three Marriage specs.** Retros shipped
2026-08-18 and Vision 2026-08-29, both walked in a browser at 15 of 15. The
Retros spec named the reason Agreements was left until last, and it was right:
"propose → both sign, with preserved history, is the genuinely hard design in
this space, and it carries a product question — what 'both' means in a household
with one owner — that deserves its own conversation rather than a corner of this
one." That conversation happened on 2026-09-05 and its answer is decision 1.

## What Agreements inherits, and what it does not

**Inherited, and used unchanged.** The `/marriage` route group with its
`requireCapability(CapMarriage)` + `requireOwner` pairing and the `requireCSRF`
sub-group every write joins; the ports.go doc-comment contract style; the
in-memory service doubles; the modal conflict-banner shape `RetroModal` and
`VisionModal` both use for a stale save; and the 403-vs-server-failure rule
`GoalsPage`, `BillsPage`, `BudgetPage` and `TransactionsPage` all now follow —
a limited member's routine "not the owner" refusal is not a red alert.

**Not inherited, and this is the whole of what makes Agreements different.**
Every other feature in this product writes when someone asks it to. This one
writes when *everyone* agrees, which means three things no existing feature
needed: a write path with two authors and a gap in time between them; a document
whose history has to survive its own edits, because the design promises a
removed agreement can still be seen and restored; and a screen whose existence
depends on the household's composition rather than on a capability. The first is
why signing is one transaction with a row lock rather than a service that writes
twice (decisions 5, 12, 13); the second is why nothing is ever deleted
(decision 9); the third is why the locked states are specified as carefully as
the working one (decisions 1, 2, 3).

**One thing genuinely absent.** No feature flag. Retros and Vision both shipped
without one, on the same reasoning: `family_calendar` exists to prove
dark-shipping works for a feature nobody has built yet, and a flag around a
finished screen is a switch nobody will ever be asked to throw.

## Decisions

1. **A household with fewer than two owners does not get Agreements.** The
   signing set is the household's owners: `domain.ValidateMembershipChange`
   refuses `CapMarriage` to a limited member, so no other member can ever be
   asked to sign. Self-serve sign-up provisions exactly one owner, with a
   partner arriving later, if at all, through an accepted invite — so this is
   the state every household starts in, not an edge case. Three answers were
   put to the product owner on 2026-09-05: let a single owner's own signature
   complete a proposal; let proposals pend until a second owner exists; or hide
   the section until there are two. The third was chosen, so **"both sign"
   means two and never one**, and there is no self-signing path to explain
   away. The cost is accepted: the design's empty state with its starter set is
   unreachable until a partner joins.

2. **The locked household still sees a screen, and it is not a wall.** The
   sidebar keeps its Agreements link and the page explains what agreements are,
   that they need two owners, and offers "Invite your partner" — a link into
   the existing Settings invite flow, not a second invite implementation.
   Hiding the link was the alternative and it teaches nobody that inviting a
   partner unlocks anything; a disabled link that cannot say why is the defect
   the admin flags screen already carries.

3. **A locked household that already has a document sees the document,
   read-only.** Two owners can become one — an owner leaves, or a membership is
   removed — and the promise those two people made does not stop existing when
   one of them does. The page renders every agreement and refuses every write,
   with a banner saying why. Only a household that has never had two owners
   sees the invite screen of decision 2.

4. **The signing set is every current owner, evaluated live.** Not literally
   two: `00009_retros.sql`'s own comment records that nothing in this product
   caps a household at two owners, and last-owner protection only guarantees at
   least one. A proposal is accepted when every owner the household has *now*
   has signed it — so an owner who joins mid-proposal is someone the promise
   binds and must sign, and an owner who leaves stops blocking it. The
   alternative, snapshotting the signer set at proposal time, means a proposal
   can be completed by people who no longer live there, which is the wrong
   answer to the question the feature exists to ask.

5. **The proposer signs implicitly, in the same transaction that writes the
   proposal.** The design's pending card says "needs Christine", not "needs
   both" — proposing *is* agreeing. That is two rows, `agreement_proposals` and
   `agreement_signatures`, and one action, so it is one repository method
   running one transaction. A proposal that exists without its proposer's
   signature would need an owner to agree to their own proposal before anyone
   else could, and nothing in the product would say so.

6. **Four states, and there is no fifth: `pending`, `parked`, `accepted`,
   `withdrawn`.** No decline, because a proposal nobody has answered is a
   conversation that has not happened yet, and a product that files it away has
   decided the conversation is over. No expiry, because a silent deletion of
   something one partner asked for is the harshest possible behaviour in a
   marriage feature. Withdrawal is the escape hatch, and it is a person's
   choice rather than a clock's.

7. **Discuss parks the proposal on the retro, and Agreements owns that data.**
   The design draws Agree and Discuss and defines only the first. Discuss moves
   a proposal to `parked`: still open, still answerable, and rendered in a
   read-only "To discuss" block on the Retros page — which is where the design's
   own agreement 07 says hard topics belong. **No writes into retro tables and
   no foreign key to a retro row**, because the next retro usually does not
   exist yet, and a feature that has to create another feature's rows to
   express itself has the boundary in the wrong place. The Retros page reads
   the same `GET /marriage/agreements` document the Agreements page reads: this
   is frontend composition, and there is no Go-level coupling between
   `RetroService` and `AgreementService` at all.

8. **Sections are labels, not agreements.** Creating one is immediate and
   unsigned — a heading is not a promise — which is exactly what the design's
   own modal does when "Create & add first agreement" takes you straight into
   Propose. An empty section is **invisible in the document** until an agreed
   agreement sits in it, so a section nobody filled never becomes clutter one
   owner must ask permission to remove.

9. **Live rows plus an append-only proposal log.** The page reads live rows
   directly; version history is the accepted-proposal log, newest first, which
   is exactly what the design's history modal renders ("Added #12 …. Agreed by
   Andreas & Christine."). **Nothing is ever deleted** — a removed agreement
   keeps its row with `removed_at` set, which is what makes the design's own
   promise true: "It stays in Version history, so you can always see it was
   there and restore it later." Two heavier shapes were considered and rejected:
   a full snapshot per version buys time travel the design never asks for and
   needs a diff step to describe a change in words, and temporal rows make every
   ordinary read of the current document carry a version predicate forever.

10. **The document version is derived, never stored.** It is
    `count(accepted proposals) + 1`. A stored counter can drift from the rows it
    counts, and this file already records that lesson where the admin metrics
    screen refused to add an analytics table for the same reason.

11. **No position columns anywhere; ordering is `created_at, id`, and the
    displayed numbering is derived at render.** `00009_retros.sql`'s comment on
    `retro_actions` gives the reasoning and it applies unchanged: an ordering
    integer needs a writer, the only safe writer is `max(position)+1` inside the
    insert, two owners can still collide on it, and the design draws no
    reordering control — so the column would exist only to create the race. The
    design's `01`–`12` runs continuously across sections and is computed when
    the page is composed; storing it would mean renumbering every later
    agreement on every removal.

12. **Signing locks the target agreement row, not the proposal row.** Locking
    the proposal serialises two signatures on *that* proposal and nothing else.
    Two different proposals against the same agreement — one editing it, one
    removing it — would then both pass their checks and both land, producing a
    duplicated agreement or a removal recorded against wording nobody agreed to.
    The lock is taken on the agreement, so changes to one agreement are ordered
    with respect to each other.

13. **A proposal carries `previous_body`, and signing re-verifies it inside the
    same transaction.** It is the target's exact wording when the proposal was
    made. It does three jobs: it is what the history modal prints for a removal,
    it is what Restore pre-fills, and it is the staleness check. If the target
    has since changed or gone, the sign refuses with
    `domain.ErrAgreementChanged` and writes nothing. Merging the two changes
    would silently produce a document neither owner agreed to.

14. **A stale proposal says so, and the sentence depends on who is reading.**
    Once another change lands on its target, the card disables Agree and
    explains: the proposer is told to withdraw it and propose again against the
    current wording; the other owner is told to ask them to. A disabled button
    with no explanation, or a live button that always 409s, are both worse than
    the refusal itself.

15. **Withdraw is the proposer's, until the proposer is no longer an owner.**
    Then any owner may withdraw it. Without that fallback a proposal left behind
    by a departed partner can never be removed by anyone, which is the
    permanently-stuck state this codebase has already shipped once, in invites.

16. **Agree stays available when the awaiting list is empty.** Because the
    signer set is evaluated live (decision 4), a proposal can become fully
    signed by nobody's action: three owners, one proposes, one agrees, the third
    leaves. Completion is only ever decided during a signing, so that proposal
    would sit pending, needing nobody, forever. The signature write is an upsert
    on `(proposal_id, membership_id)`, so one idempotent re-Agree by either
    remaining owner closes it — no background job, no sweeper.

17. **"Use starter set" seeds the four sections only.** Money, Conflict, Home &
    kids, Us — labels, no agreements. Every agreement without exception arrives
    through propose → sign, so "everything on this page is here because you both
    agreed" stays literally true, with no bulk-signed exception to explain. The
    design draws the button but never its contents, so this is an
    interpretation, recorded here rather than discovered later.

18. **Restore is an ordinary add proposal.** The version-history entry for a
    removal offers Restore, which opens Propose in add mode pre-filled with
    `previous_body`. Anything else would let one owner put back, alone, what two
    owners agreed to take out.

19. **`agreement_sections` carries `UNIQUE (household_id, name)`, mapped by
    constraint name.** Two sections called "Money" in one household is a data
    defect, and a duplicate name that surfaces as the generic
    `domain.ErrAlreadyExists` leaves the screen unable to tell "you already have
    that section" from anything else that could collide.

20. **The membership references carry no `ON DELETE` action.**
    `agreement_proposals.proposed_by_membership_id` and
    `agreement_signatures.membership_id` are log columns, the shape
    `admin_audit_log.actor_user_id` already uses. `CASCADE` — what
    `retro_action_assignees` uses — would silently un-sign an accepted agreement
    when a membership row goes away, and the document would renumber itself with
    no record of why. `RESTRICT` would make removing an owner impossible after
    their first proposal.

21. **The proposal kind is parsed at the HTTP boundary.** A kind arrives from
    two places: a request body, where a bad value is the caller's mistake and
    answers `422`, and a database column, where a bad value is a corrupt row and
    must not be reported to a household as their own typo. One sentinel serving
    both jobs makes a broken row indistinguishable from a bad request, so the
    handler parses the request value the way `parseVisionYear` already does, and
    the repository's own parse failure stays a fault.

22. **Refusal precedence is decided, not incidental: `404` → `403` → `409`.** A
    withdraw request for a proposal that does not exist is a 404; one belonging
    to someone else is `403 AGREEMENT_NOT_PROPOSER`; and a household that has
    dropped below two owners is `409 AGREEMENTS_NEED_TWO_OWNERS`. Stating the
    order matters because the handler reads the proposal before it can know
    whose it is, and "every write refuses with 409 when locked" is otherwise
    false on the wire.

## Data model

Migration `00014_agreements.sql`, following `00009_retros.sql`'s conventions. Proposals and
agreements name each other, so that cycle is closed by an `ALTER TABLE` at the end.

```sql
-- +goose Up
-- No position column anywhere below; ordering is created_at, id, for the reason 00009_retros.sql:34-41
-- gives (decision 11): the only safe writer is max(position)+1 inside the insert, two owners still
-- collide on it, and no reordering control is drawn -- the column would only create that race.
--
-- A section is a label, and creating one is immediate and unsigned (decision 8). UNIQUE (household_id,
-- name) is decision 19: the adapter maps that CONSTRAINT NAME, above the generic 23505 case, to
-- ErrAgreementSectionNameTaken, so the screen can name what collided.
CREATE TABLE agreement_sections (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- No DEFAULT now(): "Use starter set" writes four rows in one transaction, and one shared now()
    -- would tie them into a random uuid order. The writer stamps them strictly apart.
    created_at   timestamptz NOT NULL,
    UNIQUE (household_id, name)
);
-- The append-only log of every change ever proposed (decision 9): nothing is deleted and no content
-- column is rewritten -- only status, resolved_at and park_note move, so there is no updated_at.
CREATE TABLE agreement_proposals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Parsed in Go on the way out; every CHECK here is a backstop, never the enforcement.
    kind         text NOT NULL CHECK (kind IN ('add', 'edit', 'remove')),
    status       text NOT NULL CHECK (status IN ('pending', 'parked', 'accepted', 'withdrawn')),
    -- Copied from the target on an edit or a remove, so a pending edit whose target is removed has a card.
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    target_agreement_id uuid,          -- NULL for an add; its FK is added below
    body          text NOT NULL DEFAULT '',
    previous_body text NOT NULL DEFAULT '',  -- the target's wording when proposed (decision 13)
    note          text NOT NULL DEFAULT '',
    park_note     text NOT NULL DEFAULT '',
    -- A log column with NO foreign key, which is what decision 20's "no ON DELETE action" has to mean
    -- in Postgres: a bare REFERENCES defaults to NO ACTION, members are hard-deleted
    -- (queries/identity.sql:74), and an FK would then refuse to remove an owner who had ever proposed
    -- anything -- the outcome decision 20 rejects RESTRICT for. CASCADE, which retro_action_assignees
    -- uses, would silently un-sign an accepted agreement. The id is verified at write time through
    -- memberships (this household, role = 'owner'), and outlives the person leaving because the
    -- record of who agreed has to.
    proposed_by_membership_id uuid NOT NULL,
    created_at  timestamptz NOT NULL,
    resolved_at timestamptz,           -- NULL means open, retros.completed_at's shape
    CONSTRAINT agreement_proposals_resolution_matches_status  -- the two cannot disagree
        CHECK ((status IN ('pending', 'parked')) = (resolved_at IS NULL)),
    -- Validate's rule again, for statements written by hand. ELSE false so an unmatched kind fails
    -- closed: a CHECK accepts NULL.
    CONSTRAINT agreement_proposals_shape CHECK (
        CASE kind
        WHEN 'add'    THEN target_agreement_id IS NULL     AND body <> '' AND previous_body =  ''
        WHEN 'edit'   THEN target_agreement_id IS NOT NULL AND body <> '' AND previous_body <> ''
        WHEN 'remove' THEN target_agreement_id IS NOT NULL AND body =  '' AND previous_body <> ''
        ELSE false END
    )
);
-- The living document, and a row stays in it forever: removal is a stamp (decision 9) -- "it stays in
-- Version history, so you can always see it was there and restore it later".
CREATE TABLE agreements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    body         text NOT NULL,
    -- Never NULL: propose -> sign is the only path -- "everything here is here because you both agreed".
    added_by_proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id),
    removed_by_proposal_id uuid REFERENCES agreement_proposals(id),
    removed_at             timestamptz,
    created_at             timestamptz NOT NULL,
    CONSTRAINT agreements_removal_is_whole   -- a removal is one event, not half of one
        CHECK ((removed_at IS NULL) = (removed_by_proposal_id IS NULL))
);
-- Closes the cycle, at the default NO ACTION not RESTRICT: a household delete cascades into both sides
-- of it in one statement, and only NO ACTION waits for the end of that statement to check.
ALTER TABLE agreement_proposals
    ADD CONSTRAINT agreement_proposals_target_agreement_id_fkey
    FOREIGN KEY (target_agreement_id) REFERENCES agreements(id);

-- One owner's Agree. The primary key makes a double-clicked Agree idempotent (decision 16): the write
-- is an upsert and signed_at keeps the first stamp. Nothing deletes a signature either -- a departed
-- owner's is a true record that stops counting, because the set is every CURRENT owner (decision 4).
CREATE TABLE agreement_signatures (
    proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL,       -- a log column with no FK, as above
    signed_at     timestamptz NOT NULL,
    PRIMARY KEY (proposal_id, membership_id)   -- scoped to a household via the proposal
);

-- Live reads carry the predicate rather than filter a growing tail of removed rows; the second
-- index serves Document's split of the log.
CREATE INDEX agreements_household_live_idx ON agreements (household_id) WHERE removed_at IS NULL;
CREATE INDEX agreement_proposals_household_status_idx ON agreement_proposals (household_id, status);

-- +goose Down
ALTER TABLE agreement_proposals DROP CONSTRAINT agreement_proposals_target_agreement_id_fkey;  -- first
DROP TABLE agreement_signatures;
DROP TABLE agreements;
DROP TABLE agreement_proposals;
DROP TABLE agreement_sections;
```

The membership columns need a test that deletes a **membership row directly** — not a household, whose
cascade removes both sides and proves nothing — asserting the proposal, its signature and the agreement
all survive: neither cascaded nor refused, which is the whole of what decision 20's "no `ON DELETE`
action" buys.

## Ports and service

`domain/agreement.go` holds the rules that need no database, `usecase/ports.go` gains one repository and
`usecase/agreement.go` one service. Nothing takes an actor parameter to decide whether a caller may act:
services enforce what is **valid**, middleware enforces who is **asking**, and the membership id
`Propose` and `Sign` take is only ever **stored**.

### `domain/agreement.go`

| Signature | Contract |
|---|---|
| `MaxAgreementBodyLen = 500`, `MaxAgreementNoteLen = 500`, `MaxAgreementParkNoteLen = 500`, `MaxAgreementSectionNameLen = 60` | Counted in **runes**, never bytes: a household writing Chinese would otherwise get a third of what it was promised, which is how `MaxVisionThemeLen` shipped wrong once. The park note gets its own constant rather than borrowing the proposal note's: they are different fields on different screens, and one cap serving both would have to move for both |
| `MinAgreementOwners = 2` | A minimum, never a maximum — nothing caps a household at two owners, and two is the floor the product owner chose on 2026-09-05 (decision 1) |
| `ParseAgreementProposalKind(s string) (AgreementProposalKind, error)` — `add`, `edit`, `remove`<br>`ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error)` — `pending`, `parked`, `accepted`, `withdrawn`, and there is no fifth (decision 6)<br>`(s AgreementProposalStatus) IsOpen() bool` | The parsers refuse a value this code did not construct. Status only ever arrives from a column; kind also from a request body, and the **handler** parses that one and answers `422` itself (decision 21), so a corrupt row and a caller's typo never share an answer. A fourth kind needs a migration as well as a case. `IsOpen` is pending-or-parked in one function, so a fifth status cannot read as open at three call sites and not the fourth |
| `StarterSectionNames() []string` | Money, Conflict, Home & kids, Us — sections only, never agreements (decision 17) |
| `RequiredSigners(all []Membership) []string`<br>`AwaitingSignature(all []Membership, signed []string) []string`<br>`AgreementsLocked(all []Membership) bool` | The signing set is every **current** owner's membership id (decision 4); `AwaitingSignature` is those of them yet to sign, in the same order, which is what "needs Christine" is built from; `AgreementsLocked` is decision 1's gate. All three are named once so the read path's "is this screen locked" and every write path's "may this write happen" cannot drift apart. This is the read side of the rule — `Sign`'s SQL recounts owners in-transaction because only that count is atomic, and one fixture pins the two together. A signature from someone who is no longer an owner is ignored, never deleted |
| `AgreementDisplayNumbers(sizes []int) [][]int` | The `01`–`12` positions; `sizes[i]` is section i's live count. Numbering runs **continuously across sections** (Money ends at 04, Conflict starts at 05), which is why it takes the whole document's shape. Derived at render, stored nowhere (decision 11): a stored number needs rewriting after every removal. It returns integers and never pre-padded strings — the zero padding is presentation, and the browser is the only place that should own it |
| `type AgreementProposal struct{ HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note, ProposedByMembershipID }` | One proposed change before any row exists. The service stamps `HouseholdID` and `ProposedByMembershipID` from the route and the session, never from the body. `SectionID` is an add's alone; `Body` is empty on a remove; `PreviousBody` is the target's exact wording on an edit or a remove (decision 13) |
| `(p AgreementProposal) Validate() error`<br>`ValidateAgreementSectionName(name string) error` | The caps plus the kind's shape, with a refusing default on `Kind`, before any repository call — so an invalid proposal writes nothing. They check and do not trim: the service trims first, so what is **stored** is what was validated. An edit whose body equals its previous body is refused, because the version number is a promise that something happened. No cap on how many sections a household may have: a count cap is a check-then-write two owners can both pass |

### Sentinels

| Sentinel | Refuses |
|---|---|
| `ErrAgreementsNeedTwoOwners` | Decision 1's gate. In domain beside `ErrLastOwner`: a fact about the owner set, not part of a port's contract |
| `ErrAgreementChanged` | The target moved, went, or no longer reads `previous_body`. Deliberately not `ErrNotFound` — "it vanished" and "someone changed it" are different things to be told, and `ErrNotFound` on these routes means the proposal row itself |
| `ErrAgreementNotOpen` | A sign, park or withdraw against a resolved proposal, reached ordinarily by the last signer double-clicking Agree. It means "reload, this was settled", not "try again". One name for the Go side, pairing with the domain's `IsOpen()`; the wire code it maps to is `AGREEMENT_PROPOSAL_RESOLVED` |
| `ErrAgreementSectionNameTaken`, `ErrAgreementSectionNameRequired`, `ErrAgreementSectionNameTooLong`, `ErrAgreementBodyRequired`, `ErrAgreementBodyTooLong`, `ErrAgreementNoteTooLong`, `ErrAgreementParkNoteTooLong` | The unique index (decision 19 — sections are never deleted, so a name is never freed), the blanks and the caps. Every refusal gets its own sentinel, so every 422 can name a field — which is why the two notes do not share one |
| `ErrAgreementProposalShapeInvalid`<br>`ErrAgreementEditUnchanged` | The first covers every wrong combination of the four content fields — an add with no section, an edit with no target, a remove with no `previous_body` — as one sentinel like `ErrTransactionAccountsInvalid`: the modal sends one of three complete shapes, so a mismatch is a hand-built request and four codes would tell it which field to try next. The second stays separate, because there the shape is fine and the screen says something different |
| `ErrUnknownAgreementProposalKind`, `ErrUnknownAgreementProposalStatus` | A value no migration allowed. **Neither gets a `MapDomainError` case.** The kind sentinel never reaches the mapper from a request: `handleProposeAgreementChange` calls `ParseAgreementProposalKind` itself and writes `422 AGREEMENT_KIND_INVALID` at the boundary, the way `parseVisionYear` does (decision 21). So both sentinels can only reach the mapper from a column, and the logged 500 is the right answer to an impossible row |

### `AgreementRepository`

Every method filters on `householdID` in SQL: another household's row is indistinguishable from one that
does not exist. Ordering is `created_at, id` everywhere except `AgreementDocument.Accepted`, which is
`resolved_at, id` — a version is the k-th **acceptance**, and two proposals created A then B can be
accepted B then A. Three methods run transactions, so the struct holds the `*pgxpool.Pool` beside the
pool-backed `*sqlcgen.Queries`.

| Method | Contract |
|---|---|
| `Document(ctx, householdID) (AgreementDocument, error)` | The whole screen in one read: sections, live agreements (`removed_at IS NULL`), open proposals, and accepted ones, each with its signatures and a `*time.Time` `ResolvedAt`, because "still open" is a state and the zero time is not a moment. One method, because the version number, the version-history list and the retro page's To-discuss block all derive from more than one of them. Open and Accepted are two slices because they are in two orders and one slice cannot hold both; withdrawn appear in neither, excluded **in SQL**. Kind and status are re-parsed, never cast. Every slice non-nil. Unbounded on purpose: a household writes a few agreements a year |
| `Proposal(ctx, householdID, proposalID) (AgreementProposalRecord, error)` | One proposal whatever its status, withdrawn included, so the withdraw handler's proposer check costs one query rather than a composed document |
| `CreateSection(ctx, householdID, name, createdAt) (AgreementSectionRecord, error)` | A clash with `UNIQUE (household_id, name)` is `ErrAgreementSectionNameTaken`, mapped by constraint **name** (decision 19). `createdAt` is the caller's; nothing here reads a clock |
| `CreateSections(ctx, householdID, names, createdAt) ([]AgreementSectionRecord, error)` | "Use starter set" (decision 17): every name in one transaction, `ON CONFLICT DO NOTHING` so a second click is a no-op rather than a 409, then read back inside it — two of four landing would leave a household half-seeded with no button left to ask for the rest. The read-back is how the caller proves all four landed; nothing renders from its order, since `starter-set` answers with the whole freshly composed document and render order is always the document's |
| `CreateProposal(ctx, in AgreementProposalWrite) (AgreementProposalRecord, error)` | Writes the proposal row **and** the proposer's implicit signature (decision 5), verifying the target first for an edit or a remove, all in **one transaction: either all of it happens or none of it does.** A proposal without its proposer's signature would ask both owners to be the second signer of a set of one, and no route here could repair it. The target must exist in this household, still be live, and its body must equal `PreviousBody` exactly — compared as stored, never re-trimmed, since the service trims on the way in and a second trim would accept wording the proposer never saw; either failure is `ErrAgreementChanged` with nothing written. The target's `section_id` is copied onto the proposal in the same statement, which is why `Validate` refuses a caller-supplied one. The signature insert selects the proposer through `memberships` (this household, `role = 'owner'`), and zero rows rolls the transaction back with `ErrForbidden` — the backstop behind `requireOwner`; a `section_id` outside this household matches zero rows the same way and is `ErrNotFound`. Both ids come from a request body, so both pass `uuidLooksValid` first: without it `"banana"` converts to the same zero UUID an **absent** value produces and reads as "no section given" |
| `Park(ctx, householdID, proposalID, note, at) (AgreementProposalRecord, error)` | Discuss: the proposal stays open and moves to the retro page's To-discuss block (decision 7); parking twice replaces the note. One guarded `UPDATE` with the status condition in the `WHERE` clause, never a service `if`, because a check-then-write races. Zero rows is diagnosed by one re-read — gone → `ErrNotFound`, resolved → `ErrAgreementNotOpen`, the re-read's own failure passed through untouched, because "that was already settled" is a false claim to make of an unreachable database. **Nothing here touches the retro tables and there is no foreign key to a retro row**: the next retro usually does not exist yet, which is exactly when a couple parks something. This is the line an editor would try to "improve" with an FK, so the reason lives here and everywhere else cites it |
| `Withdraw(ctx, householdID, proposalID, byMembershipID, at) (AgreementProposalRecord, error)` | The same guarded `UPDATE`, plus `AND (proposed_by_membership_id = $by OR that membership is no longer an owner of this household)`: proposer-only until the proposer leaves, then any owner — without the second leg a proposal left behind by a departed partner can never be removed by anyone (decision 15 carries the reason). The clause is a **backstop**; the handler decides and answers first (see Error handling for the refusal order), and this method never branches on `$by`. **Four** diagnose legs, in order: gone → `ErrNotFound`; resolved → `ErrAgreementNotOpen`; open, someone else's, and that someone still an owner → `ErrForbidden`; the re-read itself failed → that error, folded into none of the other three |

`Sign(ctx, in AgreementSignatureWrite) (AgreementProposalRecord, error)` records one Agree and, when
that completes the signing set, applies the change — all in **one transaction, on that transaction's
own connection: either all of it happens or none of it does.** (A pool-backed call inside
`pgx.BeginFunc` takes a second connection while the first is held, and enough concurrent signers then
deadlock against `MaxConns` — the hang `VisionRepo.Save` shipped.) It is the only method that writes
an `agreements` row, and it returns the proposal as it then stands.

1. `SELECT` the proposal `FOR UPDATE`, scoped by household — missing is `ErrNotFound`, a resolved status
   `ErrAgreementNotOpen`. This lock is also what orders two owners pressing Agree on the *same*
   proposal at the same instant: the second waits here rather than racing. If the first signature
   completed the set it wakes to `accepted` and is refused; if it did not — three owners, two
   non-proposers pressing together — it wakes to `pending` and completes the change itself.
2. For an edit or a remove, **`SELECT` the target agreement `FOR UPDATE`**: same household,
   `removed_at IS NULL`, body still exactly `previous_body`. **The target is the lock that matters**
   (decision 12) — the proposal lock orders two signatures on one proposal and nothing else, so two
   proposals against the *same* agreement never contend on it, and without this one both verify and
   both apply, leaving a household that agreed to one edit with two live agreements and a removal
   recorded against wording nobody agreed to. **This `SELECT` is the only place in `Sign` that
   compares `previous_body`** — one lock, one check; `CreateProposal` makes the same comparison at
   propose time, against its own row, which is a different transaction refusing a different mistake — so any failure is `ErrAgreementChanged`, rolled back, the
   signature included. It runs on **every** signing, before the signature lands: after step 4, a
   middle signer's agreement is recorded against wording that has already moved.
3. `INSERT` the signature through `memberships` (household **and** `role = 'owner'`), `ON CONFLICT
   (proposal_id, membership_id) DO UPDATE SET membership_id = excluded.membership_id`, leaving
   `signed_at` alone. It stores nothing new but still counts a row, which is what lets `:execrows`
   tell a double-click (1 row, harmless) from a caller who is not an owner here (0 rows,
   `ErrForbidden`) — `DO NOTHING` would make the two identical.
4. Count current owners and the signatures held by current owners, both in this transaction
   (decision 4 — a service-level count cannot close the window). Complete when they are equal **and**
   owners ≥ `MinAgreementOwners`, so a household down to one owner cannot finish a change nobody is
   left to agree with; not complete, commit here with the status unchanged. The count is a READ
   COMMITTED snapshot over rows, so an owner added mid-flight need not sign this one.
5. Apply, in a switch on kind with a refusing default: **add** inserts one `agreements` row,
   **remove** stamps the target removed, **edit** does both or neither. Every stamp carries `AND
   removed_at IS NULL`, is `:execrows`, and `n == 0` returns `ErrAgreementChanged` from inside the
   transaction — step 2 should have caught it, and this is the loud failure if a future edit loses
   that lock, rather than five of six writes committing.
6. `UPDATE` the proposal to accepted, `resolved_at = At`, guarded by `AND status IN ('pending','parked')`,
   `:execrows`, and `n == 0` an error — `SetBillNextDue` shipped the other way round.

### `AgreementService`

Two ports, positional: `AgreementRepository` and the existing `MembershipRepository`. **No
owner-count port** — every pending card names who has still to sign, so the service needs those names
anyway, and the count is `len(domain.RequiredSigners(members))` off that same list, so the locked
screen and the pending card cannot disagree. Every method takes `at time.Time`; nothing here reads a
clock. `requireTwoOwners` runs first in **every** write, returning `ErrAgreementsNeedTwoOwners` —
uniform on purpose, because a rule that let some writes through a locked household is one a reader
gets wrong, and an open proposal simply waits (no expiry, no decline, decision 6) for a second owner.

| Method | Validates | Composes |
|---|---|---|
| `Get` | — | The whole screen, in one walk — the only place any of these figures is derived. `Version` is `len(Accepted) + 1` (decision 10), with `versionOf[proposalID]` recorded as it goes and read back with the comma-ok form. `UpdatedAt` is the newest accepted proposal's `ResolvedAt`, nil when `Accepted` is empty — the field `Document` already returns, so the header never has to be told separately when the document last moved. Sections keep `Document`'s order, each collecting its live agreements and accumulating `Count` **in the same walk that fills them**, so a total and its own breakdown cannot diverge — the `BudgetService.Month` defect. Display numbers come from one `AgreementDisplayNumbers` call; `Visible = Count > 0` (decision 8) and `TargetChanged` (decision 14) are decided here so the screen reads flags rather than re-deriving rules, `TargetChanged` being true for an edit or a remove whose target is gone from the live agreements or whose body no longer equals `PreviousBody` — the read-side echo of decision 13, whose authority stays in `Sign`. Four things fail closed rather than render something wrong: an open proposal whose status is neither pending nor parked, an accepted-slice row whose status is not accepted, an agreement whose `SectionID` names no section, and a `versionOf` miss — each is a row this code never wrote, and a logged 500 beats an agreement silently missing from the page or a history entry numbered v0. `versionOf` exists for exactly one consumer, each history entry's own `Version`; no agreement carries an "added in v4" badge, because the design draws none and a number nothing renders is a field that rots. History is the accepted slice reversed to newest-first, never re-sorted by a second rule, and travels **on the document itself**: it is composed from rows this walk already holds, so a second endpoint would refetch what the first threw away and go stale after every Agree. Every slice is `make(..., 0, n)` so it serialises `[]`. `Locked` gates writes and never hides the document (decision 3): a household that dropped to one owner keeps seeing what it agreed to and its frozen proposals, so the screen shows the document **and** the explanation rather than choosing. `Locked` and the owners list travel on every response — the list's length is the owner count, and no second count travels beside it — because a screen may only say a household is locked once an answered query has said so |
| `CreateSection` | trims, then `ValidateAgreementSectionName`. Never pre-checks the collision: the unique index decides it, and a "does this name exist" read is a check-then-write two owners both pass | `CreateSection` |
| `SeedStarterSections` | — | `CreateSections(domain.StarterSectionNames())` |
| `Propose` | stamps household and proposer from the route and the session **before** validating, trims body, previous body and note, then `Validate`. The target check is not repeated here: it can only be made atomically inside the write transaction | `CreateProposal` |
| `Sign` | nothing — every count and comparison that decides the outcome is inside the repository's transaction | `Sign` |
| `Park`, `Withdraw`, `Proposal` | `Park` trims the note and caps it at `MaxAgreementParkNoteLen`, refusing with `ErrAgreementParkNoteTooLong`; withdraw's proposer check is the handler's (see Error handling), and `Proposal` exists for it | `Park`, `Withdraw`, `Proposal` |

## API

Seven routes join the **existing** marriage group in `router.go` — `requireCapability(domain.CapMarriage)` on `requireOwner`,
already holding Retros and Vision, because a second group is a second place to forget a guard. The six writes sit inside its
`requireCSRF` sub-group, the single read outside it (CSRF exempts GET), and every path below is under `/api/v1`.

| Method + path | Success | Body in | Body out |
|---|---|---|---|
| `GET /marriage/agreements` | 200 | — | `agreementsResponse` |
| `POST /marriage/agreements/sections` | 201 | `createAgreementSectionRequest` | `agreementSectionWriteResponse` |
| `POST /marriage/agreements/starter-set` | 200 | — | `agreementsResponse` |
| `POST /marriage/agreements/proposals` | 201 | `proposeAgreementChangeRequest` | `agreementProposalWriteResponse` |
| `POST /marriage/agreements/proposals/{id}/agree` | 200 | — | `agreementProposalWriteResponse` |
| `POST /marriage/agreements/proposals/{id}/park` | 200 | `parkAgreementProposalRequest` | `agreementProposalWriteResponse` |
| `POST /marriage/agreements/proposals/{id}/withdraw` | 200 | — | `agreementProposalWriteResponse` |

**201 creates a row, 200 acts on one that exists**; `starter-set` is 200 because it may create nothing. **Each action is its own
POST, not a patchable `status`**, or saving a note could withdraw a proposal. Nothing is ever deleted, so **no DELETE and no
204** (decision 9) and **no restore route** (Restore is a propose with `kind: "add"`, decision 18). **There is one read and no
separate history route**: version history is a slice the service already composed for the version number (`Get`, under Ports and
service, says why a second endpoint would be both wasteful and wrong). The read is not gated on `locked` (decision 3).
```go
// propose and park only: three rune-capped free-text fields overflow the 1 KiB default
const maxAgreementRequestBodyBytes = 8 * 1024
type createAgreementSectionRequest struct {
	Name string `json:"name"`
}
type proposeAgreementChangeRequest struct {
	Kind              string `json:"kind"`              // "add" | "edit" | "remove", parsed here (decision 21)
	SectionID         string `json:"sectionId"`         // add: required. edit/remove: blanked by the handler, the section is the target's
	TargetAgreementID string `json:"targetAgreementId"` // edit/remove: required
	Body              string `json:"body"`              // add/edit: required
	PreviousBody      string `json:"previousBody"`      // edit/remove: required
	Note              string `json:"note"`              // always optional
}
type parkAgreementProposalRequest struct {
	Note string `json:"note"` // may be "", but the body is always sent: an absent one is 400 INVALID_BODY
}
```
`previousBody` is required, not optional: an agreement body is never empty, so an omitted one would always read as stale. Ids
are unchecked here: scoping is `(householdID, id)` at the repository, so a bad or foreign proposal id — the one in the path —
or a foreign `sectionId` is `ErrNotFound` → 404. A foreign or unknown `targetAgreementId` is the exception and answers
`409 AGREEMENT_CHANGED`: to this household that agreement does not exist, which is indistinguishable from one that has been
removed, and both mean the same thing to the caller — what you proposed against is not there any more. Agree and
withdraw read no body, since a client-echoed `previousBody` would be the one copy nobody verified. **`handleProposeAgreementChange`
blanks `SectionID` for any `kind` other than `add`** before it builds `domain.AgreementProposal`, because the propose modal holds
a section id from add mode and a client that forgets to clear it would get a 422 for a field the server was going to overwrite
anyway; `Validate`'s refusal of a caller-supplied section on an edit or a remove stays as the fail-closed backstop behind that.
```go
type agreementDTO struct{ ID string `json:"id"`; Number int `json:"number"`; Body string `json:"body"` } // Number is the design's "01" as an integer, derived at render (decision 11); the zero padding is the browser's. No addedAt and no signer ids on the wire: the design renders neither, and a field nothing reads is a field nothing keeps honest
// Every section travels, empty ones included (decision 8). Count is its live agreements and Visible is Count > 0, both stamped
// by the service: the page renders the visible ones and the propose picker offers them all, off ONE array. Two arrays would
// ship every section twice on every write response, for one boolean.
type agreementSectionDTO struct{ ID string `json:"id"`; Name string `json:"name"`; Count int `json:"count"`; Visible bool `json:"visible"`; Agreements []agreementDTO `json:"agreements"` }
type agreementOwnerDTO struct{ MembershipID string `json:"membershipId"`; Name string `json:"name"` }
type agreementProposalDTO struct {
	ID                     string    `json:"id"`
	Kind                   string    `json:"kind"`      // "add" | "edit" | "remove"
	Status                 string    `json:"status"`    // "pending" | "parked" in the document; a WRITE response may also carry "accepted" or "withdrawn"
	SectionID              string    `json:"sectionId"` // set for all three kinds; for edit and remove it is the target's section
	SectionName            string    `json:"sectionName"`
	TargetAgreementID      string    `json:"targetAgreementId"` // "" for add
	Body                   string    `json:"body"`              // "" for remove
	PreviousBody           string    `json:"previousBody"`      // "" for add
	Note                   string    `json:"note"`
	ParkNote               string    `json:"parkNote"`       // may be "" even when parked: Discuss is a bare button
	ProposedByMembershipID string    `json:"proposedByMembershipId"`
	ProposedByName         string    `json:"proposedByName"` // "" if the membership no longer resolves; the card omits attribution
	ProposedAt             time.Time `json:"proposedAt"`
	AwaitingNames          []string  `json:"awaitingNames"` // owners yet to sign, evaluated live (decision 4)
	TargetChanged          bool      `json:"targetChanged"` // previousBody no longer matches the live target
	CanAgree               bool      `json:"canAgree"`      // = !locked && (!signedByViewer || len(awaitingNames) == 0)
	CanWithdraw            bool      `json:"canWithdraw"`   // = !locked && (viewer proposed it || the proposer is no longer an owner)
}
type agreementsDocumentDTO struct {
	Locked    bool                       `json:"locked"`
	Owners    []agreementOwnerDTO        `json:"owners"`    // its length IS the owner count; no second count travels beside these rows
	Version   int                        `json:"version"`   // count(accepted) + 1: untouched is v1, the first change makes v2 (decision 10)
	UpdatedAt *time.Time                 `json:"updatedAt"` // the newest accepted change's acceptedAt; null until there is one
	Sections  []agreementSectionDTO      `json:"sections"`  // every slice on the wire is [], never null
	Proposals []agreementProposalDTO     `json:"proposals"` // pending and parked only; its length IS the open count
	History   []agreementHistoryEntryDTO `json:"history"`   // every accepted change, newest first — the version modal reads this, it fetches nothing
}
// One accepted change, newest first. Version is the one this change PRODUCED; SignedByNames is read from the signatures it collected, never from today's owners; and there is no display number, because a change accepted two years ago has no position in today's document.
type agreementHistoryEntryDTO struct{ Version int `json:"version"`; ProposalID string `json:"proposalId"`; Kind string `json:"kind"`; SectionID string `json:"sectionId"`; SectionName string `json:"sectionName"`; Body string `json:"body"`; PreviousBody string `json:"previousBody"`; Note string `json:"note"`; ProposedByName string `json:"proposedByName"`; SignedByNames []string `json:"signedByNames"`; AcceptedAt time.Time `json:"acceptedAt"` }
type agreementsResponse struct{ Agreements agreementsDocumentDTO `json:"agreements"` }
type agreementSectionWriteResponse struct{ Section agreementSectionDTO `json:"section"`; Agreements agreementsDocumentDTO `json:"agreements"` }
type agreementProposalWriteResponse struct{ Proposal agreementProposalDTO `json:"proposal"`; Agreements agreementsDocumentDTO `json:"agreements"` }
```
A signer whose membership no longer resolves is **omitted from `SignedByNames`** rather than joined as an empty string, or
history renders "Agreed by Andreas & " — decision 20 keeps the signature row forever, so this is the ordinary case for any
household a partner has left, not a corruption. `joinNames` of the shorter list is what the modal prints.

`canAgree`/`canWithdraw` say the caller *may* act, **not** that the write will succeed — freshness is `targetChanged`'s job —
and `canAgree`'s second clause is decision 16, without which a proposal whose awaiting list emptied by itself could never be
closed. **Every write returns the whole document**, because each moves the version, the `01..N` numbering, the history list and
which proposals are open; it is composed after the write, outside its transaction, so it is a snapshot a concurrent agree may
already have overtaken — the frontend's refetch stays the authority. `agreementProposalWriteResponse.Proposal` is the row the
write touched at the status it now holds, and the only place `accepted` or `withdrawn` reaches the wire; **no screen renders
it** — every card refetches off `Agreements` — and it exists so the HTTP tests can assert the transition field by field, which
a whole-document assertion would bury.

**The locked household** (fewer than two owners) gets 200 from the read. A household that has **never** had two owners has
nothing to send, so it answers `locked: true`, the single owner in `owners`, `version: 1`, `updatedAt: null` and empty arrays —
a consequence of its rows, not a special shape. A household that **had** two owners gets its full document with `locked: true`
(decision 3), `canAgree`/`canWithdraw` false so nothing clickable would 409. 200 rather than 403 in both cases because the empty
state *is* the page (decision 2): what they lack is a second owner, not permission. **Every write that reaches the service
refuses 409 `AGREEMENTS_NEED_TWO_OWNERS`**, withdraw and `starter-set` included — from the service, not middleware: it is a fact
about the household, not about who is asking. Its precedence against 404 and 403 is stated once, under Error handling
(decision 22). `starter-set` is idempotent — `ON CONFLICT (household_id, name) DO NOTHING` over decision 17's four labels.

**A stale proposal**'s `previous_body` no longer matches its target. Signing re-verifies it inside the signing transaction
(decision 13) and on a mismatch writes nothing, answering **409 `AGREEMENT_CHANGED`**; the same check runs at propose time for
`edit` and `remove`, or a stale proposer files a proposal nobody can sign and their partner is told they are the one out of
date; it cannot arise for `add`. A reworded target and one removed by another accepted proposal answer identically; a 404 would
be false, since the proposal *was* found and only its target moved, and `targetChanged` says so before the click (decision 14).
Acting on a proposal already `accepted` or `withdrawn` is **409 `AGREEMENT_PROPOSAL_RESOLVED`**, a separate code since nothing
was reworded. A repeat Agree is an idempotent 200 that closes the proposal if that signature completes the set, and re-parking
updates the note.

New cases go in `MapDomainError` under an `// --- Agreements ---` banner, **above** the `ErrAlreadyExists` backstop and the
`default` arm, since `errors.Is` matches in source order.

| Sentinel | Status | Code |
|---|---|---|
| `ErrAgreementsNeedTwoOwners` | 409 | `AGREEMENTS_NEED_TWO_OWNERS` |
| `ErrAgreementChanged` | 409 | `AGREEMENT_CHANGED` |
| `ErrAgreementNotOpen` | 409 | `AGREEMENT_PROPOSAL_RESOLVED` |
| `ErrAgreementSectionNameTaken` | 409 | `AGREEMENT_SECTION_NAME_TAKEN` |
| `ErrAgreementSectionNameRequired` / `ErrAgreementSectionNameTooLong` | 422 | `AGREEMENT_SECTION_NAME_REQUIRED` / `AGREEMENT_SECTION_NAME_TOO_LONG` |
| `ErrAgreementProposalShapeInvalid` | 422 | `AGREEMENT_PROPOSAL_SHAPE_INVALID` |
| `ErrAgreementEditUnchanged` | 422 | `AGREEMENT_EDIT_UNCHANGED` |
| `ErrAgreementBodyRequired` / `ErrAgreementBodyTooLong` | 422 | `AGREEMENT_BODY_REQUIRED` / `AGREEMENT_BODY_TOO_LONG` |
| `ErrAgreementNoteTooLong` / `ErrAgreementParkNoteTooLong` | 422 | `AGREEMENT_NOTE_TOO_LONG` / `AGREEMENT_PARK_NOTE_TOO_LONG` |

Answered elsewhere, and deliberately not in that table: **422 `AGREEMENT_KIND_INVALID` is written by
`handleProposeAgreementChange` itself**, from its own `ParseAgreementProposalKind` call, the way `parseVisionYear` answers a bad
year — decision 21, so a bad request body and a corrupt column never share a sentinel or an answer. Middleware gives 401
`UNAUTHENTICATED`, 403 `FORBIDDEN` and 403 `CSRF_INVALID`; 403 `AGREEMENT_NOT_PROPOSER` is compared in
`handleWithdrawAgreementProposal` against the caller **and the current owner set** (decision 15); 400 `INVALID_BODY` and 413
`PAYLOAD_TOO_LARGE` come from `decodeJSONBodyLimit`, whose doc comment gains these two callers; 404 `NOT_FOUND` covers any
unknown or foreign id. `ErrUnknownAgreementProposalStatus` is **deliberately unmapped** and logs a 500, a bad column being a
corrupt row. `AGREEMENT_SECTION_NAME_TAKEN` reaches the page only if the postgres adapter maps
`agreement_sections_household_id_name_key` by name (decision 19); an untranslated 23505 is the generic `ALREADY_EXISTS`.

## Screens and states

`/marriage/agreements`, built flat in `web/src/features/marriage/`:

| File | Job |
|---|---|
| `AgreementsPage.tsx` | Layout and screen states only. No `apiFetch` |
| `useAgreements.ts` | One `GET`, six writes, `afterWrite()` — which invalidates the one agreements key, so both the page and the Retros block re-render off a single write — and the shared `handleWriteError(err, reload, fallback)`, where `fallback` is the message shown when the error is a genuine server failure rather than one of the refusals the hook names. It calls `reload`, so it is behaviour, not copy |
| `agreementQueryKeys.ts`, `agreementSchemas.ts`, `agreementCopy.ts` | The query key alone, so the Retros block shares the cache without importing the page; Zod mirrors of the handler's DTOs — the proposal schema's `status` is **four-valued**, because a write response carries the row at the status it now holds, and a two-valued enum would fail to parse the very Agree that completes a set, which `apiFetch` turns into a thrown error on an ok response; narrowing to `pending`/`parked` is the card's own switch, never the schema's job; every string, plus `joinNames`, `agreementDateLabel` and `historyDateLabel` |
| `AgreementSectionCard.tsx` | One section: name, count, numbered rows |
| `ProposalCard.tsx` | One proposal and its actions. Mutations arrive **as props** — a hook per card would be callers racing one cache entry |
| `ProposeAgreementModal.tsx`, `NewSectionModal.tsx`, `VersionHistoryModal.tsx` | The three-mode propose editor; a section name plus suggestion chips; accepted changes with the disclosure and Restore |
| `AgreementsToDiscuss.tsx` | The read-only block on the Retros page |

Also edited: `router.tsx` (the route under `marriageGuardRoute`, plus
`validateSearch` on `settingsRoute`), `Sidebar.tsx`, `RetrosPage.tsx`,
`SettingsPage.tsx`, `MembersPanel.tsx`. Route, guard and nav entry land
**together**: `110ab0a` split them and shipped a link that 404'd.

The ladder before any content is `loading` → `403` → any other error → data.
**The 403 branch is a plain `<section>`; only the genuine failure is
`role="alert" … text-danger`** — being told a screen is owner-only is not an
incident. `RetrosPage.tsx:64-90` / `VisionPage.tsx:62-68` are that shape
verbatim: branch on the real status, never a second `useMe()` role check that
could disagree with the server (`docs/LEARNING.md` pattern 1). Everything below
renders **only from an answered query** — never `data?.locked ?? true`, which
flashes "you need a second owner" at two owners on every load.

### The four states before the document

`locked` is true below two owners (decision 1); the two locked screens are told
apart by the document itself, not a second flag. **Every write refuses while
locked (decision 22), so any proposal or history row proves the household once
had two owners** — sections alone do not, being labels, not promises (decision 8).
The unlocked pair branches on `sections.every(s => s.count === 0)` — the service's own per-section
counts, never `version - 1` and never a second total the wire would have to keep honest — and
all four keep the header and any pending proposal block.

| State | What renders |
|---|---|
| **Locked, nothing written yet** (decision 2) | The 🤝 tile, "Agreements need at least two owners", a body explaining what agreements are and that every one is agreed by all the owners, a factual "This household has one owner.", and **"Invite your partner"**. Never "both of you": the signing set is every current owner (decision 4) |
| **Locked, with content** (decision 3) | The document, read-only — the promise two people made does not stop existing when one of them leaves. **Version history stays, being a read; `+ Section`, `Propose a change`, Agree, Discuss and Withdraw are gone**, not disabled, under "This household is down to one owner, so nothing here can change. Everything you agreed is still here, and it unlocks again when a second owner joins." With `proposals.length > 0` that banner gains a second line — "1 change is waiting for a second owner" at one, "{n} changes are waiting for a second owner" above that, the verb agreeing with the count the way the header's subtitle already does — so a household can see its frozen proposals were not deleted. Nothing is deleted (decision 9) |
| **Empty, no sections** | "Write your first agreements", the body, **"Add your first agreement"**, **"Use starter set"**, then the four "Popular starting points" cards (Money, Conflict, Home & kids, Us) as read-only illustration, the design giving them no `onClick`. **Here "Add your first agreement" opens the New section modal, not Propose** — Propose's section select would be empty, the `BillsPage` dead end `docs/LEARNING.md` records, on the first screen anyone sees |
| **Empty, sections seeded** (decisions 8, 17) | Where "Use starter set" lands you: it seeds four labels and no agreements, and an empty section is invisible, so rendered naively the click changes nothing. "Your sections are ready", the body naming them in prose ("Money, Conflict, Home & kids and Us are ready. Nothing has been agreed yet…"), which proves the click worked without needing an exception to decision 8. "Use starter set" is gone; "Add your first agreement" opens Propose on the first section |

The invite deep link is new work: `validateSearch` on `settingsRoute` accepts
only literal `true` or the string `"true"` and returns `{}` otherwise, so
`?invite=maybe` fails closed; `MembersPanel` **seeds** `useState(openInvite)`
rather than binding it, a bound prop being what would reopen the modal on the
next render.

### The document

`h1` "Our agreements"; the subtitle "A living document — changes need every owner
to agree" plus " · v7, updated 28 Jun" **only when `updatedAt` is not null**,
never "v1, updated —"; "🔒 Private — parents only" in every state, including the
populated one the design drops it from; then **+ Section**, **Version history**,
**Propose a change**. The page renders `sections.filter(s => s.visible)` — one
server-stamped flag, not a rule re-derived here (decision 8) — while the Propose
picker offers the whole array, empty sections included; the rest splits by
`Math.ceil(n / 2)` into two columns, **not `grid-cols-2`**, whose row-major fill
would zig-zag the design's continuous numbering; one column below `lg`, in server
order (`created_at, id`, decision 11). Each row is
`String(number).padStart(2, "0")` in accent then the body — the integer is
composed by the service on every read and never stored (decisions 10 and 11), so
the page derives nothing and only pads.

### `ProposalCard` — pending and parked

One block above the sections grid, not the right column the design draws, because
below `lg` there is one column and the card must render with no sections at all.
Title: `Pending change — needs {joinNames(awaitingNames)}` or `Parked for the next
retro — needs …`, `joinNames` generalising the design's literal "needs Christine"
to any number of owners (decision 4). That title switches on `status` with a
**refusing default** and only ever sees `pending` or `parked`: the card renders
from `agreements.proposals`, where `accepted` and `withdrawn` are excluded in SQL
— a write response's `proposal` may carry them, and nothing renders that field.
Body, composed from the fields that exist because there is no summary column:
`{proposer} proposed adding to {section}:` and the body quoted; `…changing
{section} {NN}:` with `previousBody` struck through above the new wording;
`…removing {section} {NN}:` with `previousBody` quoted — an edit shows both, that
being the only place a signer sees what they are agreeing to change — then the
date, the note, and the `parkNote` under a "To discuss" label when parked. That
switch is on `kind` with **no guessing default** either, the Zod enum being the
fail-closed boundary, exactly as the history modal's is. Actions read the
server-stamped `canAgree`, `canWithdraw`, `awaitingNames` and `targetChanged`,
never a membership-id comparison in the browser:

| Case | Buttons |
|---|---|
| `canAgree && awaitingNames.length === 0`, **overriding every row below whenever `canAgree` is true** | **Agree, for every owner including the proposer and anyone who has already signed**, under "Everyone still here has agreed — Agree once more to make it final." Decision 16: an owner leaving can complete a signing set with nobody acting, and only an idempotent re-sign closes it — which is exactly when `canAgree`'s second clause turns Agree back on. The `canAgree` half of the condition is load-bearing rather than decorative: `canAgree` is `!locked && …`, so a locked household (decision 3) never reaches this row, where an empty `awaitingNames` alone would have put an Agree button on a page whose every write refuses |
| `canWithdraw` | **Withdraw**. The proposer's until they stop being an owner, then any owner's (decision 15) — only the handler can know that, so it is stamped where the caller is known, and the server still refuses independently |
| `canAgree` false with `awaitingNames` non-empty | No Agree — the proposer's own implicit signature (decision 5) and a signature already given both land here, and the title already says who it waits for |
| `canAgree` and `status === "pending"` | **Agree**, **Discuss** |
| `canAgree` and `status === "parked"` | **Agree**. No un-park: parking says where the conversation goes, not a state to undo before signing |
| `targetChanged` | Agree disabled, plus the note in the last subsection |

No Decline and no expiry (decision 6). Discuss expands **inside the card** —
"What you want to talk through (optional)", a textarea, **Park for next retro** /
**Cancel** — because decision 7 stores a `parkNote` that would otherwise stay
empty. Withdraw uses the same in-page two-button confirm
(`RetroModal.tsx:633-671`), never `window.confirm`. Each action owns its own
in-flight and error pair, rendered `role="alert" … text-danger`.

### The modals

**Propose** opens from one seed for all four entry points — `{ mode, sectionId?,
targetAgreementId?, body? }`, every field a pre-fill. Subtitle `{coOwnerNames}
will be asked to agree before it takes effect`, its verb agreed inside the copy
function since `joinNames` alone yields "…Christine agrees". The chips are real
radios with a visible focus ring, defaulting from the seed and never to a
hardcoded "edit"; switching mode clears the other mode's inputs, a hidden stale
value that still submits being worse than an empty one.

| Chip | Fields |
|---|---|
| **Add new** | "Add to section" — every section, empty ones included — with an inline "+ New section" opening that modal from inside this one; then "New agreement" |
| **Edit existing** | "Which agreement to edit", options labelled `{NN} · {full body}`: the design truncates, no rule is specified, and inventing one risks two agreements sharing a label. Then "New wording", pre-filled with the current body |
| **Remove** | The same select, then the design's warning verbatim but for the names — *"This will be removed once {names} agree(s). It stays in **Version history**, so you can always see it was there and restore it later."* — painted with the existing `danger-soft`/`danger-border`, as `RetroDetail.tsx:146-152` already does for the design's warm card. The verb agrees with the count, as the subtitle's does |

"Why (optional)" renders in all three modes. Footer **Cancel** / **Send for
agreement**, both `type="button"` with `onSubmit` prevented, because a
`type="submit"` once let Enter anywhere in a form finish a retro; `maxLength`
mirrors the server's cap as a courtesy, the rune-counting server being the
authority.

**Propose answers `409 AGREEMENT_CHANGED` of its own**, on an edit or a remove
whose target moved between opening the modal and sending it (decision 13; see
Error handling). This modal is the one component here that holds a draft, so it
is where the component-local, one-way `hadConflict` latch lives, read from
`err.code` and never from the query hook's derived flag — that flag clears on the
next background refetch and would re-enable **Send for agreement** with fields
typed against wording that has since changed, which is exactly the defect
`RetroModal.tsx:101` and `VisionModal.tsx:470` latch to prevent. The modal stays
open with everything typed intact, disables the send button, and says "This
agreement changed while you were writing, so nothing was saved. Close this and
start again from the current wording."

| Modal | Copy and behaviour |
|---|---|
| **New section** | "New agreement section", "Sections just group related agreements — add your own beyond the starters", one input (placeholder "e.g. In-laws, Faith, Health, Careers"), then "Or pick a suggestion" and five chips (In-laws & family, Faith & values, Health, Careers, Screens & tech) that **fill the input rather than submit**. Footer **Cancel** / **Create & add first agreement**: creating a section is immediate and unsigned (decision 8), and on success this closes and opens Propose in add mode on the new section. A duplicate name says "You already have a section called that" — decision 19's constraint mapping |
| **Version history** | "Version history", "Every change, and who agreed". It renders `agreements.history`, which arrives **newest first on the document itself** and costs no second request, so opening the modal fetches nothing and cannot show a version the page below it disagrees with; the content block scrolls, not the panel (`VisionModal.tsx:653-665`; the design's `hb-scroll` class does not exist in `web/src`). Each row is a dot, the version label, the date **with its year** (the proposal card's has none, but history spans years by construction), one derived sentence — `Added to / Changed in / Removed from {section}: "{excerpt}"`, switched on `kind` with **no guessing default**, the Zod enum being the fail-closed boundary — then `Agreed by {joinNames(signedByNames)}` |
| **…its disclosure** | The current row is the one whose `version === doc.version`, **by value, never by array position**. The four newest render, the rest collapse behind `Show v{lowest}–v{highest} ↓`, both bounds taken from the collapsed entries the way `retroCopy.ts`'s `showOlderYear` takes both from data, or `Show v{n} ↓` when one collapses. There is no v1 row: a document is v1 before anything is agreed, so the first accepted proposal produces v2 (decision 10). The reveal fetches nothing |
| **…and Restore** | On `remove` entries only. Closes the modal (nothing here stacks `<dialog>`s) and opens Propose seeded `{ mode: "add", body: previousBody, sectionId }` — an ordinary add proposal needing everyone's agreement (decision 18), saying so above the footer: "Restoring is an ordinary proposal — it takes effect once everyone agrees." Its section always still exists, sections never being removed. An `edit` entry gets no Restore: that is an edit back, one click away through the ordinary flow |

### "To discuss" on the Retros page (decision 7)

`AgreementsToDiscuss` reads the same `GET /marriage/agreements` through the same
hook and key, owning its own hook call as `VisionCard` owns its own `useVision()`.
**Frontend composition only**: nothing writes into retro tables, nothing links a
proposal to a retro row — the reasoning sits on the `Park` contract, where
somebody would try to add the foreign key — and `RetroService` and
`AgreementService` share no port and no type. It mounts as a **sibling of
`RetrosPage`'s `noRetrosYet` ternary**, not inside either branch, because the
household most likely to have parked something has not started a retro yet.
Loading renders nothing; an error renders one muted line, since silence reads as
having nothing to show. It lists parked proposals only — the same summary, the
`parkNote`, and **Agree** through the same mutation, so both screens refresh
together — while Discuss and Withdraw do not: this is a reminder of what the retro
should cover, not a second editing surface. A locked household still sees the
parked rows, with no Agree button (decision 3).

### Staleness, and the guard that moves under the page

`targetChanged` turns decision 13's refusal into a state the card states up front
rather than one somebody trips over, and the note branches on who is reading
(decision 14): whoever may withdraw gets "This no longer matches the agreement it
was written against, so it can't be agreed. Withdraw it and propose the change
again."; everyone else gets "Ask {proposedByName} to withdraw it and propose it
again against the current wording." When `proposedByName` is `""` — a locked
household whose proposer has left, the one case that reaches this branch with no
name — the sentence drops the address: "This needs withdrawing and proposing
again against the current wording." The card body does the same, opening
"Proposed: …" where it would have said "{proposer} proposed …", the same rule
the pending card follows when attribution is missing. Branching on who proposed it instead would
tell a household whose proposer left to ask a ghost — `canWithdraw` already
carries decision 15's fallback. A real `409 AGREEMENT_CHANGED` sets the same
sentence prefixed with "…so nothing was signed", then refetches.

**No `hadConflict` latch in `ProposalCard`, deliberately.** The latch belongs to
`ProposeAgreementModal`, which holds a draft; Agree holds none, so the refetch is
the whole fix: the refreshed document carries `targetChanged: true`, and the card
renders disabled for every owner from then on. `409 AGREEMENTS_NEED_TWO_OWNERS`
— an owner removed between paint and click — refetches too, and the page flips to
the read-only locked document, which explains the state better than a line under
one button. Both live in `handleWriteError`, so the rule cannot reach five writes
out of six.

## Error handling

| Case | Answer | What the screen says |
|---|---|---|
| An owner leaves while a proposal is open | Nothing is written. The signing set is evaluated live (decision 4), so they are gone from the next read and the next signing; existing signatures stand | The "waiting for" line loses a name |
| A second or third owner joins while a proposal is open | The signing set grows. Nothing is invalidated and no signature is re-collected; the new owner joins the awaiting list | The card waits for one more person |
| The household drops below two owners with proposals open | `GET` is `200` with the whole read-only document, `locked: true` and the one remaining owner in `owners`; every write refuses `409 AGREEMENTS_NEED_TWO_OWNERS`. Proposals freeze in place — decision 6 has no decline and no expiry, so they wait | Decision 3's banner, the agreements still rendered, the frozen-proposal line off `proposals.length`, singular at one, and no write control at all |
| Every remaining owner has already signed (three owners, one leaves) | The awaiting list is empty and the proposal is **still pending**: acceptance is a write and a write needs a caller. Agree stays offered (decision 16); the repeat signature is an idempotent upsert and the same transaction completes the change | The card stays actionable rather than stalling forever |
| Two proposals target the same agreement | Both are legal; the first to complete applies. The second is refused `409 AGREEMENT_CHANGED` at its next signature and writes nothing. It is **not** auto-withdrawn — decision 6 has no "superseded" status, and inventing one would be a write nobody asked for | Decision 14's two sentences: withdraw and re-propose if it is yours, ask them to if it is not |
| The proposer signs again, or Agree is double-clicked | The upsert keeps the first `signed_at` and the transaction re-evaluates completeness, so in a two-owner household the set is still incomplete and nothing moves | `200`, and the card is unchanged |
| Two owners press Agree on the same proposal at the same instant | `Sign` step 1's `SELECT … FOR UPDATE` on the proposal orders them, so one waits rather than racing. If the first signature completed the set, the second wakes to `accepted` and is refused **`409 AGREEMENT_PROPOSAL_RESOLVED` before its signature is written**, writing nothing. If it did not — three owners, two non-proposers pressing together — both land and the second completes the change | The applied change, or an error line and a refetch that shows it already applied |
| Agree on a proposal withdrawn while the page was open | The same `409 AGREEMENT_PROPOSAL_RESOLVED` — deliberately not a no-op success, because `accepted` means the household got what the click asked for and `withdrawn` means they did not. Status is read on the transaction's own connection **before** the signature is written, so a non-completing signature in a three-owner household is refused too | The card's error line, and the list refetches |
| A proposer on a stale page proposes an edit or a remove | Propose verifies `previous_body` against the live row inside its own transaction and refuses `409 AGREEMENT_CHANGED`, writing neither row | The propose modal keeps everything typed, latches its send button off, and says so |
| A non-owner reaches a route | `requireCapability(domain.CapMarriage)` refuses `403` first — a limited member can never hold marriage — and `requireOwner` is stacked behind it | The owner-only explanation, not an alert |
| Restore into a section that is now empty | Works, with no special path: sections are never removed. The section is invisible in the document (decision 8) but still travels, with `visible: false` and `count: 0`, so the propose picker can offer it — one list the service stamps, never two lists or a client filter | The picker names the section it came from |
| A duplicate section name | `UNIQUE (household_id, name)` (decision 19), mapped by constraint name to `409 AGREEMENT_SECTION_NAME_TAKEN`, never the generic `ALREADY_EXISTS`. Case-sensitive, as `categories_household_id_name_key` already is | Under the name field; the modal stays open with the typed name intact |
| A parked proposal when no retro exists yet | The To-discuss block renders anyway, as a **sibling** of the Retros page's no-retros-yet branch and never inside it — see `Park` for why no foreign key to a retro exists to break here | The parked proposal, agreeable from there |
| The proposer is no longer an owner and someone wants the proposal gone | Decision 15: any current owner may withdraw it. The handler tests the proposer against the **live owner set**, not a column that went null — decision 20 gives the membership references no `ON DELETE` action, so nothing about the proposal changes when a membership does | Withdraw appears for whoever is left |
| A `kind` or `status` this build does not know | From a request body: `422 AGREEMENT_KIND_INVALID`, refused by the handler's own parser (decision 21). From a database column: the logged `500` its sentinel gets by having no mapped case | A field message, or the generic failure banner |

**Refusal precedence is `404` → `403` → `409`, and this is the one place it is stated in full**
(decision 22). The withdraw handler must read the proposal before it can know whose it is, so the
order is a property of the handler and not of the guards: an unknown or foreign id is `404` even in a
locked household; another owner's proposal is `403 AGREEMENT_NOT_PROPOSER`; and only a request past
both reaches `409 AGREEMENTS_NEED_TWO_OWNERS`. Without the order written down, "every write refuses
with `409` when locked" is false on the wire, and a test asserting it would be pinning the wrong rule.

**Both write paths are one transaction.** Propose writes the proposal and the proposer's own
signature together (decision 5) — without it, its author would sit in their own awaiting list, offered
an Agree button for a change they wrote. Sign reads the proposal's status, locks the target and
re-verifies `previous_body` on that locked row, writes the signature, and only then applies the
change. A signature recorded against a change that never landed is this feature's worst failure: a
promise both owners agreed to, nowhere on the page, unagreeable again.

**`AGREEMENT_CHANGED` sets a component-local, one-way `hadConflict` latch in
`ProposeAgreementModal`** — the only component here holding a draft — read from `err.code`, never the
query hook's derived flag, which clears on a background refetch and re-enables the button with stale
local fields over fresh server state. `ProposalCard` deliberately has no latch: it holds nothing to
lose, and the refetch alone leaves Agree disabled through `targetChanged`. **Every `2xx` carries a
JSON body and no `204` needs carving out** — withdraw and park are transitions, each its own `POST` —
and every write answers the whole freshly composed document, since it moves which proposals are open,
the numbering and the To-discuss list as well as the row it touched.

## Testing

**Domain.** Table tests, no doubles. Both parsers refuse `""`, `"Add"`, `"delete"`, `"remove "`, and
the `"declined"` and `"expired"` decision 6 deliberately lacks, because the refusing `default` is the
point. Caps are runes, so a 500-rune Chinese body passes and a 501-rune one does not, and text is
trimmed before it is measured; the park note is capped separately from the proposal note, so a fixture
at each boundary proves the two constants are not one. Validation refuses an add with no section, an
edit with no target and a remove with no `previous_body` — three tests, all asserting the single
`ErrAgreementProposalShapeInvalid`, which is the point of it being one sentinel rather than three; an
edit whose body equals its previous body asserts `ErrAgreementEditUnchanged` instead. The owner and
awaiting helpers need fixtures carrying limited members, or they test the function minus its filter.

**Service, against in-memory doubles** (`usecase_test`, external). The double implements **every**
port method, including ones no test calls (Liskov), and counts writes, so a refusal can be proved to
precede any repository call; the service takes `at time.Time` rather than holding a clock. Tests:
the locked read still carries the whole document; every write on a one-owner household is refused
with `writes == 0`; propose signs the proposer implicitly; the awaiting list grows when an owner
joins and shrinks when one leaves, still pending after the shrink; the last awaiting signature
applies an add, an edit (whose proposal keeps the old wording, which is what history renders) and a
remove; the version counts accepted proposals only and the `Proposals` slice carries pending and
parked only, from one fixture carrying all four statuses, so `count(*)` and a pending-only
implementation both fail; `UpdatedAt` is nil with no accepted proposal and the newest acceptance's
timestamp with three; numbering runs continuously across two sections and skips a removed row;
withdraw on an accepted proposal writes nothing; the starter set is idempotent and creates **no**
agreements; and a section holding no live agreements arrives with `Visible` false and `Count` zero
rather than being dropped, since the picker must still offer it.

**Postgres, under testcontainers**, a fresh container per test. The two atomicity tests need their
fault on the **second** write or there is nothing for a partial write to leak: propose, and sign,
against a section in another household, then read back and find neither row, with `AcquiredConns()
== 0` afterwards — the only assertion that can see a missing rollback. One runs with the pool
starved to a single free connection, which is what proves that **every** statement of `Sign` — the
status read, the target lock, the signature upsert, the apply and the status update — runs on the
transaction's own connection rather than reaching back into the pool: a pool-backed call inside
`pgx.BeginFunc` takes a second connection while the first is still held, and with one connection
free it deadlocks instead of passing quietly. The three refusal legs are three tests, one per predicate of step 2's
`SELECT … FOR UPDATE` — target in another household, target removed, target reworded — each
asserting `ErrAgreementChanged`, so a mutation cannot go red on the wrong one;
and a **three-owner** fixture is needed twice, for a non-completing signature on a withdrawn
proposal that must be refused and write nothing, and for a repeat signature that must keep the first
`signed_at`, asserted by value not row count. Also: agreeing an accepted proposal writes the
agreement row once; the proposal log is read back field by field, never `count(*)`, with the park
note arriving through the write path; ordering is proved with three rows, two sharing a
`created_at`; the section unique key holds against raw SQL as well as through Go; and deleting a
signer's membership row directly leaves the proposal, its signature and the agreement in place —
neither cascaded nor refused (decision 20).

**HTTP** (`httpadapter_test`, external). A per-file `addSecondOwner` helper seeds the second owner
and signs in for real cookies, kept out of `newTestEnv`, whose household other files' owner-count
assertions depend on. The guard matrix walks all seven routes against no session, a limited member
and an owner; the CSRF test sends all six writes twice — no header, then a wrong one — asserting the
**code**, since a bare status check stays green with the `requireCSRF` line deleted; and a doctored
limited-member-holding-marriage identity is the only thing that can see `requireOwner`, because both
guards answer `403 FORBIDDEN`. Then: the locked read is `200`, its empty arrays asserted on the
**raw wire bytes**, since Go decodes `null` and `[]` identically; every write answers a parseable
JSON body; withdraw is tested in both directions, because a comparison with its sides swapped passes
every refusal test; a stale agree through two real sessions is `409 AGREEMENT_CHANGED` and the
proposal is still listed after it; an unknown `kind` is `422 AGREEMENT_KIND_INVALID` from the
handler's own parser rather than a 500; and an oversized body is `413`.

**All three router-wide walks are re-measured from their own `t.Logf` output, never bumped by hand**
— the CSRF walk, the unauthenticated walk, and `TestOwnerOnlyRoutesRejectALimitedMember` in
`household_api_test.go`, the one most easily missed; one of these floors has already drifted stale
here. Its limited member holds money and not marriage, so the agreements routes satisfy it at
`requireCapability` as retro and vision already do — which is why the doctored-membership test stays
the only proof `requireOwner` is wired.

**Frontend.** `stubFetchRoutes` for every request, including `/auth/me` for the session — the Agree
button is gated by the server's `canAgree`, never by a membership id compared in the browser — and
**every existing Retros page test gains the agreements route in the same change**, because an
unregistered route throws, TanStack Query absorbs the throw into error state, and a component that
renders null on missing data stays green while silently erroring — three times inside Retros alone.
Then: all five page states, each its own test; the locked explanation proved absent **while the query
is in flight**, by holding the response open, since the first render passes vacuously; every write
invalidating the agreements key, and both mounted components re-rendering off that one invalidation,
since the To-discuss block shares the key rather than being told separately; bodies asserted with
`toEqual` against the whole body, since `toMatchObject` would pass a propose that dropped its note;
`ProposeAgreementModal`'s latch staying disabled across a successful refetch; and the To-discuss
block rendering nothing when nothing is parked, and rendering the parked rows with no Agree button
when locked. Finally, grep every mutation the hook returns for a caller outside it — Goals shipped
archive-and-restore with no screen calling it — and assert agree's two call sites separately.

**Mutation checks.** Break the code, watch it go red *for the expected reason*, restore. An orphaned
import gives a build failure, which prints the same word as a test failure — say which you saw.

| Mutation | Must go red | Watch for |
|---|---|---|
| The owner-count guard from `>= 2` to `>= 1` | The one-owner refusal test, on `writes == 0` | If only the error assertion fires, the guard has moved *after* the repository call. The counter is what separates "refused" from "refused eventually" |
| Delete the proposer's signature insert from propose | The implicit-signature test | If only the atomicity test fires, the two rows are written but not together — a different defect |
| Delete `AND body = $previous_body` from `Sign` step 2's `SELECT … FOR UPDATE` | The **sign-time** reworded-target test — named, because the propose-time one stays green under this mutation and would satisfy a loosely worded check — and the **removed-target test must stay green**, since step 2's `removed_at IS NULL` survives the deletion | If both go red, decision 13's two claims are checked in one place and the split into two tests is fictional |
| Delete the status read that precedes the signature upsert | The three-owner non-completing-sign-on-a-withdrawn-proposal test | Every two-owner fixture stays green, because there every non-proposer signature is the completing one. That is why the fixture has three owners |
| `count(accepted)` → `count(*)` in the version derivation | The version test | Only if the fixture carries pending, parked **and** withdrawn proposals; with accepted ones alone the numbers agree and the mutation is invisible |
| Hardcode `Visible = true` on every section | The empty-section test | Only if the fixture has a section holding no live agreements, and only if the test asserts the flag rather than the array length — the section travels either way |
| `ProposeAgreementModal`'s one-way `hadConflict` latch replaced by the hook's derived flag | The propose-modal test asserting the send button stays disabled **after a refetch** | A test that only asserts the banner appeared stays green — every existing conflict test here did, which is how this bug reached a browser once already |
| Delete `requireOwner` from the marriage group | The doctored-membership tests — this one and the existing marriage one | Nothing in the guard matrix may go red. If it does, the matrix was passing for the wrong reason |

**The browser walk, fifteen criteria**, against a real database before anything is called done. Two
cautions: this machine runs two Docker engines, so check `lsof` on 5173 first; and `make seed` leaves
the second owner as a *pending invite*, so a freshly seeded household **is** the locked state.

| # | Criterion | Why it is here |
|---|---|---|
| 1 | A freshly seeded household explains the two-owner rule, and "Invite your partner" lands on Settings **with the invite modal open**; `?invite=maybe` lands with it closed | The deep link is this feature's own new work, so the walk must prove the modal opens rather than accept a bare navigation to Settings |
| 2 | With the network throttled, a **two-owner** household never flashes the locked explanation on a cold load | Only an answered query may say a household is locked; the flash is invisible to a walk that navigates and waits |
| 3 | Accept the invite in a second browser profile: the page is the unlocked empty state | The transition every household will actually make |
| 4 | Click "Use starter set". The page does **not** look identical afterwards, and the four sections are reachable in the propose picker | Decision 8 makes an empty section invisible, so this catches a button that appears to do nothing |
| 5 | Propose as the first owner: the card says who it waits for, offers Withdraw, offers no Agree. Withdraw one and watch it go | Decision 5; the design never drew the proposer's own view |
| 6 | The same card as the second owner offers Agree and Discuss, and no Withdraw | The half the design did draw |
| 7 | Agree it: the agreement appears numbered in its section, and the section count, the continuous numbering and the header version all move together | A total and its breakdown must apply the same filter or they quietly stop reconciling |
| 8 | Discuss a proposal, open Retros, agree it from the To-discuss block. **Both** pages reflect it with no manual reload | Decision 7, and the proof that one shared query key is enough |
| 9 | On a household that has never started a retro, the To-discuss block still renders | Two mutually exclusive branches; a block inside either is invisible in the other |
| 10 | Two browsers, one agreement, two edit proposals: the first goes through, the second is refused with the conflict copy, nothing typed is lost, the proposal is still there | Decision 13, and the propose modal's latch. jsdom cannot express two sessions |
| 11 | Propose a remove, agree it, open version history, read the removed wording and restore it — the picker offers the now-empty section | Decisions 9 and 18; the design promises restore and draws no control for it |
| 12 | With proposals open, remove the second owner in Settings: the page re-locks, the proposals are **still listed** and named as waiting, no write control is offered | A household that loses sight of its proposals will assume they were deleted |
| 13 | Create a section named "Money" twice: the second says so under the field, the modal stays open, the typed name survives | A generic red box four clicks in is the empty-`<select>` failure again |
| 14 | Keyboard only: Tab reaches Agree, Discuss and Withdraw on a pending card and every control in the propose modal, each with a visible focus ring; Enter activates | `fireEvent.click` never presses a key, which is how unreactive `sr-only` radios shipped |
| 15 | The ladder 320 / 360 / 375 / 414 / 768 / 1024 / 1440 on the populated page and the history modal, comparing `scrollWidth` to `clientWidth` | A numbered row plus a section label plus body text plus two buttons is the shape that has overflowed before |

Compare screenshots by hash, not by eye — a previous round's fix was caught half-wrong by two
byte-identical screenshots that looked different. Then walk it once off-script: a walk derived
entirely from a spec has already passed here while the spec was wrong.

Deliberately **not** tested: concurrent database load (the starved pool proves only that every
statement of `Sign` runs on the transaction's own connection), and layout at any width, since criterion 15 is measured
by hand.

## Out of scope

- **The behaviour behind the design's four "Popular starting points" cards.** The cards themselves are
  built, as read-only illustration on the first empty state — the design draws hover styling and gives
  them no `onClick`, and nothing says what tapping one would propose. So the drawing ships and the
  interaction does not; the tracker records the behaviour as drawn-but-not-built, the treatment the
  ⌘K chip already has.
- **Declining a proposal, and expiring one** (decision 6). Filing away or deleting an unanswered
  proposal decides the conversation is over; withdrawal is the escape hatch, and a person's choice
  rather than a clock's.
- **Reordering, renaming or removing a section** (decisions 8 and 11). No position column and no
  control drawn, so one would exist only to create the race two owners can lose; an empty section is
  already invisible, and a rename touches wording two owners agreed to, so it would need its own
  propose-and-sign story.
- **Bulk import, and signing an existing document in one go** (decision 17). Every agreement arrives
  through propose → sign, which is what keeps "everything here is here because you both agreed"
  literally true, with no exception to explain.
- **Any notification when a proposal is waiting.** The other owner learns of it when they open the
  app. ADR 3 keeps mail on the box, and ADR 4 deliberately limits the Telegram channel to URLs — "no
  balances, no names, not even a household id" — so neither channel can carry "Christine proposed
  raising the no-questions amount". A proposal notification needs a channel that can carry household
  content, and that does not exist yet; building one is a decision about what this product is willing
  to send off the box, not a corner of this spec.

## Definition of done

`make lint && make test` green on the integrated tree, at least one
mutation-checked test, the fifteen-criterion browser walk passed and recorded in
`docs/superpowers/plans/`, and the three tracking documents updated **in the same
round**:

- **`docs/SYSTEM_DESIGN.md`** — the Marriage section gains four tables, seven
  routes and the propose → sign flow, which is the first request flow in this
  product with two actors and a gap between them. Use the
  `maintaining-system-design` skill; change the prose under the diagram, not
  only the diagram.
- **`docs/FEATURE_TRACKER.md`** — checked against the section 6 table as it
  stands, which holds seven ⬜ rows, six of them this feature's:

  | Row | After |
  |---|---|
  | Agreements by section | ✅ |
  | Agreements empty state with starter sets | ✅, with a note that the starter set seeds sections only (decision 17) |
  | Propose a change — add, edit, remove (modal) | ✅ |
  | New agreement section (modal) | ✅ |
  | Version history (modal) | ✅ |
  | Agreements locked until a second owner | ✅ — the row added 2026-09-05, ahead of the code, when decision 1 was settled |

  The seventh, "Vision — marriage duration beside the theme", stays ⬜: it is
  Vision's, deliberately unbuilt, and nothing here touches it. Two new rows the
  design never draws are added by this work — **park a proposal for the next
  retro** (decision 7) and **restore a removed agreement from version history**
  (decision 18) — the same treatment Goals' contributions and Accounts'
  archive/restore already have. A third row records what is drawn and not built:
  the four "Popular starting points" cards ship as illustration, so the row is
  **their behaviour**, ⬜ with the reason in its own cell — the design gives them
  no `onClick` and nothing says what tapping one would propose.

  Recount the summary table by counting symbols, never by adjusting the previous
  totals — that has produced wrong numbers in this file before, in both
  directions at once.
- **`docs/LEARNING.md`** — what this round taught, added to an existing pattern
  where one fits rather than starting a new section. Two candidates are already
  visible from the design review: the row-lock defect (locking the wrong row
  serialises the wrong thing) belongs with the existing concurrency evidence,
  and "the proposer's implicit signature is a second row, so proposing is a
  transaction" is more evidence for pattern 1, which
  `guarding-partial-writes` already carries four instances of.
