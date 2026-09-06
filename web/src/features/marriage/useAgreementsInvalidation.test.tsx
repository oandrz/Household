// One row per write the hook returns. The claim is identical for all six and
// worth stating once: a write does not trust the document it got back -- it
// invalidates the one key, and the refetch is what both screens render. A
// missing onSuccess leaves a screen showing a document one write out of date,
// which is invisible to any test that asserts only the write's own return value.
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useAgreements } from "./useAgreements";
import type { AgreementsDocument } from "./agreementSchemas";

const DOC_URL = "/api/v1/marriage/agreements";
const OWNERS = [
  { membershipId: "mem-1", name: "Andreas" },
  { membershipId: "mem-2", name: "Christine" },
];

function documentFixture(version: number): AgreementsDocument {
  return {
    locked: false, owners: OWNERS, version, updatedAt: null,
    sections: [], proposals: [], history: [],
  };
}

const SECTION = { id: "sec-1", name: "Money", count: 0, visible: false, agreements: [] };
const PROPOSAL = {
  id: "prop-1", kind: "add" as const, status: "accepted" as const, sectionId: "sec-1",
  sectionName: "Money", targetAgreementId: "", body: "No solo spend over $200", previousBody: "",
  note: "", parkNote: "", proposedByMembershipId: "mem-1", proposedByName: "Andreas",
  proposedAt: "2026-09-05T09:00:00+08:00", awaitingNames: [], targetChanged: false,
  canAgree: false, canWithdraw: false,
};

function renderUseAgreements() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useAgreements(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

type Write = {
  name: string;
  route: string;
  status: number;
  body: unknown;
  call: (hook: ReturnType<typeof useAgreements>) => Promise<unknown>;
};

const writes: Write[] = [
  {
    name: "createSection",
    route: `POST ${DOC_URL}/sections`,
    status: 201,
    body: { section: SECTION, agreements: documentFixture(2) },
    call: (h) => h.createSection("Money"),
  },
  {
    name: "seedStarterSet",
    route: `POST ${DOC_URL}/starter-set`,
    status: 200,
    body: { agreements: documentFixture(2) },
    call: (h) => h.seedStarterSet(),
  },
  {
    name: "propose",
    route: `POST ${DOC_URL}/proposals`,
    status: 201,
    body: { proposal: PROPOSAL, agreements: documentFixture(2) },
    call: (h) =>
      h.propose({
        kind: "add", sectionId: "sec-1", targetAgreementId: "",
        body: "No solo spend over $200", previousBody: "", note: "",
      }),
  },
  {
    name: "agree",
    route: `POST ${DOC_URL}/proposals/prop-1/agree`,
    status: 200,
    body: { proposal: PROPOSAL, agreements: documentFixture(2) },
    call: (h) => h.agree("prop-1"),
  },
  {
    name: "park",
    route: `POST ${DOC_URL}/proposals/prop-1/park`,
    status: 200,
    body: { proposal: { ...PROPOSAL, status: "parked" as const }, agreements: documentFixture(2) },
    call: (h) => h.park("prop-1", "Let us talk about the number"),
  },
  {
    name: "withdraw",
    route: `POST ${DOC_URL}/proposals/prop-1/withdraw`,
    status: 200,
    body: { proposal: { ...PROPOSAL, status: "withdrawn" as const }, agreements: documentFixture(2) },
    call: (h) => h.withdraw("prop-1"),
  },
];

describe("useAgreements — every write invalidates the one key", () => {
  it.each(writes)("$name refetches the document after it lands", async ({ route, status, body, call }) => {
    const fetchMock = stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture(1) } },
        { status: 200, body: { agreements: documentFixture(2) } },
      ],
      [route]: { status, body },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data?.version).toBe(1));

    await act(async () => {
      await call(result.current);
    });

    // The cache now holds the REFETCHED document, not the write's own response.
    await waitFor(() => expect(result.current.data?.version).toBe(2));
    expect(
      fetchMock.mock.calls.filter(
        ([input, init]) => String(input) === DOC_URL && (init?.method ?? "GET") === "GET",
      ),
    ).toHaveLength(2);
  });
});
