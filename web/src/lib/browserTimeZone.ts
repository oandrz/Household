// The IANA time zone this device is set to, such as "Asia/Singapore".
//
// It has exactly one use: sign-up sends it so that a new household starts on
// the calendar of the person creating it. Never use it to work out a date
// inside the app. Every date there comes from the household's stored zone
// (householdDate.ts), so that a member who is travelling still sees the
// household's day.
//
// An empty string means the browser could not say. The server refuses that
// rather than guessing, and the sign-up form then asks the person to choose.
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
}
