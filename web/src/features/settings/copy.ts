// Copy-composition helpers for the Settings screen, kept in a plain .ts
// module for the same reason features/auth/copy.ts is -- so eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports.
import { limitedAccessClause } from "../auth/copy";
import type { TelegramLinkStatus } from "./schemas";

// The Members panel's description line reads "Parent · full access" for an
// owner and "Kid · calendar & chores only" / "Kid · calendar only" for a
// limited member -- the design's own words (Settings screen, Members
// panel), distinct from copy.ts's roleLabel ("co-owner"/"Kid"), which is
// this same design's wording for the *invite-acceptance* screen instead.
function memberRoleDescriptor(role: string): string {
  if (role === "owner") return "Parent";
  if (role === "limited") return "Kid";
  return role;
}

export function memberDescriptionLine(role: string, capabilities: string[]): string {
  const descriptor = memberRoleDescriptor(role);
  if (role === "owner") return `${descriptor} · full access`;
  return `${descriptor} · ${limitedAccessClause(capabilities)}`;
}

// The pill at the right edge of each member row: "Owner" / "Limited",
// exactly as the design's Settings screen writes them (distinct from the
// descriptor above, which is lower-case-first "Parent"/"Kid").
export function memberBadgeLabel(role: string): string {
  if (role === "owner") return "Owner";
  if (role === "limited") return "Limited";
  return role;
}

// A Space carries `visibility` and an optional `requiredCapability`, but no
// single "audience" field -- the design's three builtin rows ("Parents",
// "🔒 Parents only", "Everyone") are derived from both together:
// VisibilityParentsOnly is the structural lock (Marriage); a
// requiredCapability without that lock reads as "Parents" only because
// every capability-gated builtin space in this household happens to be
// parent-held today (Money), not because the domain forbids a limited
// member from ever holding one -- there is no literal per-space audience
// field to read a stricter answer from. Documented here rather than
// asserted with more confidence than the data supports.
export function spaceAudienceLabel(space: {
  visibility: string;
  requiredCapability?: string;
}): string {
  if (space.visibility === "parents_only") return "🔒 Parents only";
  if (space.requiredCapability) return "Parents";
  return "Everyone";
}

// The symbol now comes from GET /api/v1/currencies rather than a list
// maintained here -- one list, and it lives in the backend. Callers that have
// not loaded the currency list yet pass nothing and get the bare code, which is
// what an unrecognised code always rendered as.
export function currencyLabel(code: string, symbol?: string): string {
  return symbol ? `${code} (${symbol})` : code;
}

// telegramPollInterval is TanStack Query's refetchInterval rule for the
// pending-link status query, pulled out as its own named, directly-testable
// function: "waiting" and "pending" are the only statuses that can still
// change on their own (the bot has not answered yet, or the person has not
// clicked Confirm yet) -- every other status is terminal, and polling a
// nonce that can never change again just spends the member's battery and
// this API's rate limit for nothing.
export function telegramPollInterval(
  status: TelegramLinkStatus["status"] | undefined,
): number | false {
  return status === "waiting" || status === "pending" ? 3000 : false;
}

// linkedAt arrives as an ISO string (see telegramBindingSchema); this is the
// only place it is ever turned into something a person reads.
export function formatTelegramLinkedAt(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

// chatUsername is `omitempty` on the wire and optional in the schema because
// it genuinely can be empty: the bot adapter's senderName (update.go) is the
// chat's @username, or "" when Telegram sent none -- deliberately never a
// first name, which is attacker-chosen and would let a stranger's chat forge
// the look of the expected @handle. Decision 7 exists to give the person one
// piece of evidence to check a confirm against -- a bare "@" would quietly
// lose that evidence instead of admitting there is none, so this names the
// gap rather than hiding it.
export function telegramChatLabel(chatUsername: string | undefined): string {
  return chatUsername ? `@${chatUsername}` : "a Telegram chat with no username";
}
