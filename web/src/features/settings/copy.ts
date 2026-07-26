// Copy-composition helpers for the Settings screen, kept in a plain .ts
// module for the same reason features/auth/copy.ts is -- so eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports.
import { limitedAccessPhrase } from "../auth/copy";

// The Members panel's description line reads "Parent · full access" for an
// owner and "Kid · calendar & chores only" / "Kid · calendar only" for a
// limited member -- the design's own words (Settings screen, Members
// panel), distinct from copy.ts's roleLabel ("co-owner"/"Kid"), which is
// this same design's wording for the *invite-acceptance* screen instead.
export function memberRoleDescriptor(role: string): string {
  if (role === "owner") return "Parent";
  if (role === "limited") return "Kid";
  return role;
}

export function memberDescriptionLine(role: string, capabilities: string[]): string {
  const descriptor = memberRoleDescriptor(role);
  if (role === "owner") return `${descriptor} · full access`;
  return `${descriptor} · ${limitedAccessPhrase(capabilities)} only`;
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

// "SGD (S$)" -- only the currencies this household's design and seed data
// actually use. An unrecognised code renders as the bare code rather than
// guessing at a symbol the backend hasn't told us.
const CURRENCY_SYMBOLS: Record<string, string> = {
  SGD: "S$",
  IDR: "Rp",
  USD: "$",
};

export function currencyLabel(code: string): string {
  const symbol = CURRENCY_SYMBOLS[code];
  return symbol ? `${code} (${symbol})` : code;
}
