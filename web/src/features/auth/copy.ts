// Small copy-composition helpers shared by the auth screens. Kept in a plain
// .ts module (not alongside a component) so eslint's
// react-refresh/only-export-components rule never has to think about them.
import { ApiError } from "../../api/client";

// A mutation's onError handler receives `unknown`, not just ApiError -- a
// network rejection or a schema-parse failure inside the mutationFn reaches
// it too. Filtering those out and rendering nothing (as an earlier version
// of this screen did) leaves the caller with no visible feedback at all, so
// every caller of this must always get a string back: the server's own
// message when it's an ApiError, and a generic fallback otherwise.
export function apiErrorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

// A deliberately loose "does this look like an email address" check, not a
// full RFC 5322 validator: it exists only to catch an empty field and the
// obviously-not-an-email case ("no", "andreas") before either magic-link
// control ever fires a request, both of which are type="button" outside the
// <form> and so never get the browser's own `required` validation the
// password field's sibling inputs do. A server-side check still has the
// final word on any address this lets through -- this is a client-side
// guard against wasting one of the household's three hourly magic links (or
// posting an empty email) on something that was never going to work, not a
// replacement for that check.
const PLAUSIBLE_EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isPlausibleEmail(email: string): boolean {
  return PLAUSIBLE_EMAIL.test(email.trim());
}

const NUMBER_WORDS: Record<number, string> = {
  0: "Zero",
  1: "One",
  2: "Two",
  3: "Three",
  4: "Four",
  5: "Five",
  6: "Six",
  7: "Seven",
  8: "Eight",
  9: "Nine",
  10: "Ten",
};

// The design's wrong-password copy never pluralises "1 tries left" -- it
// reads "One try left". Any count above the word table falls back to the
// numeral rather than throwing, since the lockout policy's MaxAttempts is
// configuration, not a compile-time constant.
export function triesLeftPhrase(remaining: number): string {
  const word = NUMBER_WORDS[remaining] ?? String(remaining);
  const noun = remaining === 1 ? "try" : "tries";
  return `${word} ${noun} left`;
}

// "Money, Marriage and Family" -- the design's own list style (no Oxford
// comma).
export function formatList(items: string[]): string {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0];
  if (items.length === 2) return `${items[0]} and ${items[1]}`;
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

// The three builtin spaces (api/internal/domain/space.go's BuiltinSpaces):
// Family carries no required capability and is visible to everyone; Money is
// gated on the "money" capability; Marriage is VisibilityParentsOnly, so it
// is never shared with anyone who isn't joining as an owner, regardless of
// capability.
export function sharedSpaceNames(role: string, capabilities: string[]): string[] {
  const names: string[] = [];
  if (capabilities.includes("money")) names.push("Money");
  if (role === "owner" && capabilities.includes("marriage")) names.push("Marriage");
  names.push("Family");
  return names;
}

// The design's own words for a limited member's capability grant (Settings
// members list: "Kid · calendar & chores only" / "Kid · calendar only").
const LIMITED_CAPABILITY_LABELS: Record<string, string> = {
  calendar: "calendar",
  chores: "chores",
  money: "money",
};

export function limitedAccessPhrase(capabilities: string[]): string {
  const granted = capabilities
    .filter((c) => c in LIMITED_CAPABILITY_LABELS)
    .map((c) => LIMITED_CAPABILITY_LABELS[c]);
  if (granted.length === 0) return "no";
  return granted.join(" & ");
}

// "co-owner" is the design's own word for an owner accepting an invite (the
// sign-in screen's Invited state). "Kid" is the design's own word for a
// limited member (the invite-creation modal's Role field and the Settings
// members list). Any other role value is one this screen does not yet know
// how to describe, so it is shown as-is rather than guessed at.
export function roleLabel(role: string): string {
  if (role === "owner") return "co-owner";
  if (role === "limited") return "Kid";
  return role;
}
