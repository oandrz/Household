// Fetch orchestration for the Agreements document: one GET, six writes, and
// the one query key both screens read -- AgreementsPage and the Retros page's
// To-discuss block, which owns its own useAgreements() call the way VisionCard
// owns its own useVision().
//
// Every write invalidates that key rather than trusting its own response. Each
// response does carry the whole freshly composed document, but the service
// composes it AFTER its transaction commits and outside it, so a concurrent
// Agree may already have overtaken the snapshot -- the refetch stays the
// authority. Invalidating is also what makes one write refresh BOTH mounted
// screens: they share the key rather than being told about each other.
//
// Returned field names are TanStack's own (isLoading, isProposing,
// isCreatingSection) rather than useVision.ts's `loading`, because six
// mutations sit beside the query here and one naming family across all seven
// flags is easier to read than two.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { agreementsQueryKey } from "./agreementQueryKeys";
import {
  agreementProposalWriteResponseSchema,
  agreementSectionWriteResponseSchema,
  agreementsResponseSchema,
  type AgreementKind,
  type AgreementSection,
  type AgreementsDocument,
} from "./agreementSchemas";

const BASE = "/api/v1/marriage/agreements";

// Mirrors proposeAgreementChangeRequest (agreement_handlers.go). Every field is
// always sent, empty where the kind does not use it: the handler blanks
// sectionId itself for an edit or a remove, and an OMITTED previousBody would
// always read as stale, since an agreement body is never empty.
export type ProposeBody = {
  kind: AgreementKind;
  sectionId: string;
  targetAgreementId: string;
  body: string;
  previousBody: string;
  note: string;
};

async function fetchAgreements(): Promise<AgreementsDocument> {
  const raw = await apiFetch<unknown>(BASE);
  return agreementsResponseSchema.parse(raw).agreements;
}

// The four proposal routes share one envelope and each answers the whole
// document: every one of them moves the version, the 01..N numbering, the
// history list and which proposals are open -- not only the row it touched.
// The `proposal` half of the envelope is parsed (so a drifted status fails
// loudly here rather than three screens later) and then dropped: no component
// in this feature renders it.
async function postProposalAction(path: string, body?: unknown): Promise<AgreementsDocument> {
  const raw = await apiFetch<unknown>(path, {
    method: "POST",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return agreementProposalWriteResponseSchema.parse(raw).agreements;
}

// One failed write -> the sentence to show, plus a refetch wherever the answer
// is "the page you clicked is out of date". It calls reload, so it is
// behaviour and lives here rather than in agreementCopy.ts; `fallback` is each
// caller's own copy for a genuine server failure.
//
// AGREEMENT_SECTION_NAME_TAKEN deliberately does NOT refetch: the New-section
// modal stays open with the typed name intact, and nothing about the document
// changed.
//
// ProposalCard keeps no conflict latch: it holds no draft, and the refetch
// brings back targetChanged: true, which disables Agree for every owner from
// then on. The latch belongs to ProposeAgreementModal (Task 13), which does
// hold one.
export function handleWriteError(
  err: unknown,
  reload: () => Promise<void>,
  fallback: string,
): string {
  if (!(err instanceof ApiError)) return fallback;
  switch (err.code) {
    case "AGREEMENT_CHANGED":
      // Not awaited: this function answers with a sentence synchronously, and
      // the refetch it starts lands through the query cache like any other.
      void reload();
      return AGREEMENT_COPY.writeErrorChanged;
    case "AGREEMENTS_NEED_TWO_OWNERS":
      // An owner removed between paint and click. The refetch flips the page
      // to the read-only locked document, which explains the state far better
      // than a line under one button.
      void reload();
      return AGREEMENT_COPY.writeErrorLocked;
    case "AGREEMENT_PROPOSAL_RESOLVED":
      void reload();
      return AGREEMENT_COPY.writeErrorResolved;
    case "AGREEMENT_SECTION_NAME_TAKEN":
      return AGREEMENT_COPY.sectionNameTaken;
    default:
      return fallback;
  }
}

export function useAgreements() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: agreementsQueryKey(), queryFn: fetchAgreements });

  // RETURNED, not fired and forgotten: TanStack awaits whatever onSuccess
  // returns before the mutation settles, so a caller's await lands on a
  // refreshed cache rather than racing it.
  const afterWrite = () => queryClient.invalidateQueries({ queryKey: agreementsQueryKey() });

  const createSectionMutation = useMutation({
    mutationFn: async (
      name: string,
    ): Promise<{ section: AgreementSection; agreements: AgreementsDocument }> => {
      const raw = await apiFetch<unknown>(`${BASE}/sections`, {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      // Both halves, unlike every other write: Task 14's "Create & add first
      // agreement" seeds the Propose modal with the new section's id.
      return agreementSectionWriteResponseSchema.parse(raw);
    },
    onSuccess: afterWrite,
  });

  const starterSetMutation = useMutation({
    mutationFn: async (): Promise<AgreementsDocument> => {
      // No body, and 200 rather than 201: the starter set is idempotent and a
      // second click may create nothing, so it answers the plain document
      // envelope rather than a write envelope with no row to name.
      const raw = await apiFetch<unknown>(`${BASE}/starter-set`, { method: "POST" });
      return agreementsResponseSchema.parse(raw).agreements;
    },
    onSuccess: afterWrite,
  });

  const proposeMutation = useMutation({
    mutationFn: (body: ProposeBody) => postProposalAction(`${BASE}/proposals`, body),
    onSuccess: afterWrite,
  });

  const agreeMutation = useMutation({
    // Agree and withdraw send no body: a client-echoed previousBody would be
    // the one copy nobody verified.
    mutationFn: (id: string) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(id)}/agree`),
    onSuccess: afterWrite,
  });

  const parkMutation = useMutation({
    mutationFn: (v: { id: string; note: string }) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(v.id)}/park`, { note: v.note }),
    onSuccess: afterWrite,
  });

  const withdrawMutation = useMutation({
    mutationFn: (id: string) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(id)}/withdraw`),
    onSuccess: afterWrite,
  });

  return {
    data: query.data,
    // v5's isLoading is `isPending && isFetching` -- true only while the first
    // fetch for this key is in flight with no cached value, so the background
    // refetch after a write does not blank the screen.
    isLoading: query.isLoading,
    error: query.error,
    reload: async () => {
      await query.refetch();
    },
    createSection: (name: string) => createSectionMutation.mutateAsync(name),
    seedStarterSet: () => starterSetMutation.mutateAsync(),
    propose: (body: ProposeBody) => proposeMutation.mutateAsync(body),
    agree: (proposalId: string) => agreeMutation.mutateAsync(proposalId),
    // Two arguments at the call site, one object on the wire: a card calls
    // park(id, note) and does not have to know this mutation takes a pair.
    park: (proposalId: string, note: string) => parkMutation.mutateAsync({ id: proposalId, note }),
    withdraw: (proposalId: string) => withdrawMutation.mutateAsync(proposalId),
    // The two in-flight flags a modal needs to disable its own submit button
    // (Tasks 13 and 14). The four proposal-card writes each own their own
    // in-flight pair locally, per card, since one shared flag would disable
    // every card's buttons whenever any one of them was mid-write.
    isProposing: proposeMutation.isPending,
    isCreatingSection: createSectionMutation.isPending,
  };
}
