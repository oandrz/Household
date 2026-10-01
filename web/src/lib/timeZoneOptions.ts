// The list a time zone <select> offers: every IANA zone this browser knows,
// each labelled with its name and its current offset from UTC.

type TimeZoneOption = { zone: string; label: string };

// The browser's own list of zones. It can be missing entirely on an old
// browser, in which case the select still works with UTC and whatever zone
// the caller says must be there.
function supportedZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
}

// "GMT+8" for a zone at this instant. Empty when the browser cannot format
// the zone, which is the case for a stored name it has never heard of.
function offsetLabel(zone: string, now: Date): string {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: zone,
      timeZoneName: "shortOffset",
    }).formatToParts(now);
    return parts.find((p) => p.type === "timeZoneName")?.value ?? "";
  } catch {
    return "";
  }
}

// mustInclude names zones that have to be in the list whatever the browser
// says: the household's stored zone, above all. A <select> whose value is not
// one of its options silently shows its first option instead, and the
// household would look like it is somewhere it is not.
//
// UTC is always offered. The browser's list leaves it out, and it is the zone
// a household has when no other was given.
export function timeZoneOptions(mustInclude: string[] = [], now: Date = new Date()): TimeZoneOption[] {
  const zones = new Set(["UTC", ...supportedZones(), ...mustInclude.filter((zone) => zone !== "")]);
  return [...zones].sort().map((zone) => {
    const offset = offsetLabel(zone, now);
    // The zone database spells a space as an underscore.
    const name = zone.replaceAll("_", " ");
    return { zone, label: offset ? `${name} (${offset})` : name };
  });
}
