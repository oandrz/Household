import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../../api/client";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AGREEMENT_COPY } from "./agreementCopy";
import type { AgreementProposal, AgreementsDocument } from "./agreementSchemas";
import { handleWriteError, useAgreements } from "./useAgreements";

// These four are lifted into web/src/features/marriage/agreementFixtures.tsx
// by Task 10, which needs the same shapes for the page tests and for the
// Retros block in Task 16; this file's copies are deleted in that same task.
// Keep the names identical so that edit is an import, not a rewrite.
const DOC_URL = "/api/v1/marriage/agreements";
const OWNERS = [
  { membershipId: "m-1", name: "Andreas" },
  { membershipId: "m-2", name: "Christine" },
];

function documentFixture(o: Partial<AgreementsDocument> = {}): AgreementsDocument {
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

function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
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

function renderUseAgreements() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useAgreements(), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useAgreements", () => {
  // Two claims, one click. The status enum is FOUR-valued: an Agree that
  // completes the signing set answers with the row at "accepted", and apiFetch
  // throws on an ok response it cannot parse -- so a two-valued enum would fail
  // the very click that finished the agreement. And there is ONE key: the
  // Retros To-discuss block reads the same query, so counting the GETs is what
  // proves the write invalidated that key rather than updating its own caller
  // in place.
  it("parses the accepted proposal an Agree answers with, and invalidates the one key both screens read", async () => {
    const stub = stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture({ proposals: [proposalFixture()] }) } },
        { status: 200, body: { agreements: documentFixture({ version: 2 }) } },
      ],
      [`POST ${DOC_URL}/proposals/p-1/agree`]: {
        status: 200,
        body: {
          proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
          agreements: documentFixture({ version: 2 }),
        },
      },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data?.version).toBe(1));

    let after: AgreementsDocument | undefined;
    await act(async () => {
      after = await result.current.agree("p-1");
    });

    // The write resolves to the DOCUMENT, not the row it touched.
    expect(after?.version).toBe(2);
    await waitFor(() => expect(result.current.data?.version).toBe(2));
    expect(stub.mock.calls.filter(([input]) => String(input) === DOC_URL)).toHaveLength(2);
  });

  // The one write that resolves to more than the document: Task 14's
  // "Create & add first agreement" closes this modal and opens Propose seeded
  // with the new section's id, which only this response carries. The body is
  // asserted with toEqual against the WHOLE body -- toMatchObject would pass a
  // request that silently sent extra fields the server would then refuse.
  it("createSection resolves to the new section AND the document, and posts only the name", async () => {
    let postBody: unknown;
    const created = { id: "s-9", name: "In-laws", count: 0, visible: false, agreements: [] };
    stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture() } },
        { status: 200, body: { agreements: documentFixture({ sections: [created] }) } },
      ],
      [`POST ${DOC_URL}/sections`]: {
        status: 201,
        body: { section: created, agreements: documentFixture({ sections: [created] }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data).toBeDefined());

    let out: { section: { id: string }; agreements: AgreementsDocument } | undefined;
    await act(async () => {
      out = await result.current.createSection("In-laws");
    });

    expect(postBody).toEqual({ name: "In-laws" });
    expect(out?.section.id).toBe("s-9");
    expect(out?.agreements.sections).toHaveLength(1);
    await waitFor(() => expect(result.current.data?.sections).toHaveLength(1));
  });

  // Which refusals get a named sentence, and -- separately -- which of them
  // refetch. AGREEMENT_SECTION_NAME_TAKEN deliberately does NOT: the modal
  // stays open with the typed name intact (spec, Error handling), and a
  // refetch there would be a request nobody needs. Anything that is not an
  // ApiError at all (a network fault, a Zod parse failure on an ok response)
  // takes the caller's own fallback.
  it("handleWriteError names each refusal, refetches only where the page is out of date, and falls back on anything else", () => {
    const reload = vi.fn(async () => {});

    expect(handleWriteError(new ApiError(409, "AGREEMENT_CHANGED", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorChanged);
    expect(handleWriteError(new ApiError(409, "AGREEMENTS_NEED_TWO_OWNERS", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorLocked);
    expect(handleWriteError(new ApiError(409, "AGREEMENT_PROPOSAL_RESOLVED", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorResolved);
    expect(reload).toHaveBeenCalledTimes(3);

    expect(handleWriteError(new ApiError(409, "AGREEMENT_SECTION_NAME_TAKEN", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.sectionNameTaken);
    expect(handleWriteError(new ApiError(500, "INTERNAL", "…"), reload, "fallback")).toBe("fallback");
    expect(handleWriteError(new TypeError("network down"), reload, "fallback")).toBe("fallback");
    expect(reload).toHaveBeenCalledTimes(3);
  });
});
