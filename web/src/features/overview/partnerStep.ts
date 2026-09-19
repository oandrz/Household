// Where a household is on "Invite your partner". Only owners count: the step
// exists to get a household to the two owners Agreements needs to unlock, not
// to count kids. A plain .ts module rather than inside SetupChecklist.tsx, so
// react-refresh's only-export-components rule has nothing to object to.
import type { MemberView, PendingInvite } from "../settings/schemas";

export type PartnerStep = "none" | "invited" | "joined";

export function partnerStep(members: MemberView[], invites: PendingInvite[]): PartnerStep {
  if (members.filter((m) => m.role === "owner").length >= 2) return "joined";
  if (invites.some((i) => i.role === "owner")) return "invited";
  return "none";
}
