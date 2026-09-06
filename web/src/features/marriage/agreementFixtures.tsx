// The Agreements feature's shared test fixtures. Every frontend task from here
// on (11-16) builds its stubs from these rather than retyping a seventeen-field
// proposal, so one field renamed on the wire is one edit here, not seven.
//
// WRAPPED vs INNER matters and is stated on each export: the API wraps every
// body ({ agreements } for a read, { section, agreements } and
// { proposal, agreements } for the writes), so documentFixture/proposalFixture
// return the INNER object a component sees, while emptyDoc/seededDoc return the
// ENVELOPE, because they are passed straight into a stub's `body:`.
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import type { Me } from "../auth/schemas";
import { AgreementsPage } from "./AgreementsPage";
import type {
  AgreementOwner,
  AgreementProposal,
  AgreementSection,
  AgreementsDocument,
} from "./agreementSchemas";

export const DOC_URL = "/api/v1/marriage/agreements";
export const ME_URL = "/api/v1/auth/me";

// m-1 is also meFixture's membership id below, so the viewer of every page
// test is Andreas -- the first owner and, in proposalFixture, the proposer.
export const OWNERS: AgreementOwner[] = [
  { membershipId: "m-1", name: "Andreas" },
  { membershipId: "m-2", name: "Christine" },
];
export const ONE_OWNER: AgreementOwner[] = [{ membershipId: "m-1", name: "Andreas" }];

// domain.StarterSectionNames(), mirrored: "Use starter set" seeds these four
// labels and no agreements (decision 17).
export const STARTER_SECTION_NAMES = ["Money", "Conflict", "Home & kids", "Us"];

export function meFixture(overrides: Partial<Me> = {}): Me {
  return {
    user: {
      id: "u-andreas",
      email: "andreas@hearth.family",
      displayName: "Andreas",
      avatarInitial: "A",
    },
    household: {
      id: "h-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m-1",
      householdId: "h-1",
      userId: "u-andreas",
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
    ...overrides,
  };
}

// INNER. Two owners, so the default document is unlocked; a locked fixture
// passes { locked: true, owners: ONE_OWNER } explicitly, because the two travel
// together on the wire and a fixture that set only one of them would be a shape
// the server cannot produce.
export function documentFixture(o: Partial<AgreementsDocument> = {}): AgreementsDocument {
  return {
    locked: false,
    owners: OWNERS,
    version: 1,
    updatedAt: null,
    sections: [],
    proposals: [],
    history: [],
    ...o,
  };
}

// INNER. proposedAt/updatedAt carry a real offset: 2026-06-28T21:18:52+08:00 is
// 13:18 UTC, so agreementDateLabel reads "28 Jun" in every timezone from UTC-13
// to UTC+10 -- the date assertions below do not depend on the runner's TZ.
export function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return {
    id: "p-1",
    kind: "add",
    status: "pending",
    sectionId: "s-1",
    sectionName: "Money",
    targetAgreementId: "",
    body: "One shared account for bills",
    previousBody: "",
    note: "",
    parkNote: "",
    proposedByMembershipId: "m-1",
    proposedByName: "Andreas",
    proposedAt: "2026-09-05T10:00:00+08:00",
    awaitingNames: ["Christine"],
    targetChanged: false,
    canAgree: true,
    canWithdraw: false,
    ...o,
  };
}

// INNER. A section with nothing agreed in it: count 0 and visible false, which
// is what the server stamps (decision 8) and what makes a seeded starter set
// change nothing on screen unless the page says so in prose.
export function emptySection(name: string): AgreementSection {
  return { id: `s-${name}`, name, count: 0, visible: false, agreements: [] };
}

// WRAPPED -- goes straight into a stub's `body:`. An unlocked household with no
// sections at all: the "Write your first agreements" state.
export function emptyDoc(o: Partial<AgreementsDocument> = {}): { agreements: AgreementsDocument } {
  return { agreements: documentFixture(o) };
}

// WRAPPED. Where "Use starter set" lands: four labels, no agreements.
export function seededDoc(o: Partial<AgreementsDocument> = {}): { agreements: AgreementsDocument } {
  return { agreements: documentFixture({ sections: STARTER_SECTION_NAMES.map(emptySection), ...o }) };
}

// Mounts AgreementsPage with GET /auth/me already stubbed. The page calls
// useMe(), and stubFetchRoutes THROWS on an unregistered request -- so a test
// that forgot the session stub would fail on a missing route rather than on
// what it meant to assert. Caller routes are spread last, so a test that needs
// a different member (Task 13's co-owner names) overrides the same key.
export function renderPage(routes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const stub = stubFetchRoutes({
    [`GET ${ME_URL}`]: { status: 200, body: meFixture() },
    ...routes,
  });
  return { stub, ...renderWithRouter(<AgreementsPage />) };
}
