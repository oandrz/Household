// Time labels and copy for the households pages, in their own module so
// AdminHouseholdsPage.tsx and AdminHouseholdPage.tsx export only components
// (eslint-plugin-react-refresh's only-export-components rule) and so the
// boundaries below can be unit-tested without rendering anything.

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// relativeTimeLabel is the coarse text shown in a table cell; the exact
// instant belongs in the element's title (exactTimeLabel). null is "never":
// the API sends null when no session has ever existed, and "never" is the
// honest word for it on a page whose job is "is anyone using this". A
// timestamp in the future (clock skew between browser and server) reads
// "just now" too, because every negative elapsed value falls into the first
// branch below -- keep that branch first.
export function relativeTimeLabel(iso: string | null, now: Date): string {
  if (iso === null) return "never";
  const elapsed = now.getTime() - new Date(iso).getTime();
  if (elapsed < MINUTE) return "just now";
  if (elapsed < HOUR) return `${Math.floor(elapsed / MINUTE)} min ago`;
  if (elapsed < DAY) return `${Math.floor(elapsed / HOUR)} h ago`;
  const days = Math.floor(elapsed / DAY);
  if (days === 1) return "yesterday";
  if (days < 30) return `${days} days ago`;
  if (days < 365) {
    const months = Math.floor(days / 30);
    return months === 1 ? "1 month ago" : `${months} months ago`;
  }
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

// Same locale retroCopy.completedDateLabel uses, with the year: an
// operator's list spans months.
export function dateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function exactTimeLabel(iso: string): string {
  return new Date(iso).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export function noMatchLabel(q: string): string {
  return `Nothing matches '${q}'.`;
}

export function showingLabel(
  shown: number,
  truncated: boolean,
  atCap: boolean,
): string {
  if (!truncated) return `Showing ${shown} of ${shown}`;
  if (atCap) return `Showing the first ${shown} — search to narrow`;
  return `Showing the first ${shown}`;
}

export function lockoutLabel(lockedUntilIso: string, now: Date): string {
  const minutes = Math.max(
    1,
    Math.ceil((new Date(lockedUntilIso).getTime() - now.getTime()) / MINUTE),
  );
  const time = new Date(lockedUntilIso).toLocaleTimeString("en-US", {
    hour: "numeric",
    minute: "2-digit",
  });
  return `Sign-in is locked until ${time} (in ${minutes} min).`;
}
