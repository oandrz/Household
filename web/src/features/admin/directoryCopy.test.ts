import { describe, expect, it } from "vitest";
import { relativeTimeLabel } from "./directoryCopy";

const now = new Date("2026-09-02T12:00:00Z");
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();
const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;

describe("relativeTimeLabel", () => {
  it("says never for null", () => {
    expect(relativeTimeLabel(null, now)).toBe("never");
  });
  it("walks the boundaries", () => {
    expect(relativeTimeLabel(ago(10_000), now)).toBe("just now");
    expect(relativeTimeLabel(ago(5 * minute), now)).toBe("5 min ago");
    expect(relativeTimeLabel(ago(3 * hour), now)).toBe("3 h ago");
    expect(relativeTimeLabel(ago(25 * hour), now)).toBe("yesterday");
    expect(relativeTimeLabel(ago(4 * day), now)).toBe("4 days ago");
    expect(relativeTimeLabel(ago(45 * day), now)).toBe("1 month ago");
    expect(relativeTimeLabel(ago(400 * day), now)).toBe("1 year ago");
  });
  it("never goes negative for a timestamp slightly in the future (clock skew)", () => {
    expect(
      relativeTimeLabel(new Date(now.getTime() + 5000).toISOString(), now),
    ).toBe("just now");
  });
});
