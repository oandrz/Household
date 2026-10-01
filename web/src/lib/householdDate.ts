// The household's calendar: which day, month and year it is right now in the
// time zone the household keeps (me.household.timezone), not in the zone this
// browser happens to be set to.
//
// A household has one calendar (ADR 12). The server works out "today" in the
// household's zone, so the app must too, or a form defaults to a date the
// server then refuses, and a page names a month the server is not showing.
// A member who is travelling sees the household's date, on purpose.
//
// Every "what is the date" question in the app goes through this file. Don't
// write `new Date().getFullYear()` or `toISOString().slice(0, 10)` in a
// component: the first reads the browser's zone and the second reads UTC, and
// each is right only for some households at some hours.
//
// An unusable zone throws (Intl's own RangeError). It never falls back to the
// browser's zone: that would bring back the wrong date without a sign of it.

type DateParts = { year: string; month: string; day: string };

// formatToParts, not a formatted string cut up: the order and separators of
// a formatted date belong to the locale, the named parts do not.
function partsIn(zone: string, now: Date): DateParts {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    calendar: "gregory",
    numberingSystem: "latn",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const read = (type: Intl.DateTimeFormatPartTypes) => {
    const part = parts.find((p) => p.type === type);
    if (!part) throw new Error(`householdDate: no ${type} for time zone "${zone}"`);
    return part.value;
  };
  return { year: read("year"), month: read("month"), day: read("day") };
}

// Today in the household's zone, as "YYYY-MM-DD" -- the shape a date input
// holds and the API takes.
export function todayIn(zone: string, now: Date = new Date()): string {
  const { year, month, day } = partsIn(zone, now);
  return `${year}-${month}-${day}`;
}

// This month in the household's zone, as "YYYY-MM" -- the shape
// GET /budgets/{month} and a month input take.
export function monthIn(zone: string, now: Date = new Date()): string {
  const { year, month } = partsIn(zone, now);
  return `${year}-${month}`;
}

// This year in the household's zone.
export function yearIn(zone: string, now: Date = new Date()): number {
  return Number(partsIn(zone, now).year);
}

// The name of this month in the household's zone: "October".
export function monthNameIn(zone: string, now: Date = new Date()): string {
  return new Intl.DateTimeFormat("en-US", { timeZone: zone, month: "long" }).format(now);
}

// Today in the household's zone, written out for a person to read and check
// against their own calendar: "Thu, 1 Oct 2026".
export function longDateIn(zone: string, now: Date = new Date()): string {
  // Assembled from named parts, so the order is ours and only the words come
  // from the locale. en-US, like every other month name in the app: en-GB
  // abbreviates September as "Sept".
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    weekday: "short",
    day: "numeric",
    month: "short",
    year: "numeric",
  }).formatToParts(now);
  const read = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)?.value ?? "";
  return `${read("weekday")}, ${read("day")} ${read("month")} ${read("year")}`;
}

// The month before a "YYYY-MM" month: "2026-10" -> "2026-09", "2027-01" ->
// "2026-12". Worked out from the two numbers, never through a Date: a Date
// is built in the browser's zone, and this file exists so that zone decides
// nothing.
export function monthBefore(month: string): string {
  const [year, monthNumber] = month.split("-").map(Number);
  if (monthNumber === 1) return `${year - 1}-12`;
  return `${year}-${String(monthNumber - 1).padStart(2, "0")}`;
}
