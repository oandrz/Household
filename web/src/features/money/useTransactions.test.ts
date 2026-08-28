import { describe, expect, it } from "vitest";
import { toQueryString } from "./useTransactions";

describe("toQueryString", () => {
  it("returns an empty string when no filter is set", () => {
    // Not "?" -- a bare "?" would still be a truthy suffix on the fetch path
    // and is the kind of thing a naive `params.toString()` join can produce.
    expect(toQueryString({})).toBe("");
  });

  it("omits an absent filter rather than sending it empty or as 'undefined'", () => {
    // Only `month` is set. A looser implementation (e.g. always calling
    // params.set for every key) would emit `?kind=undefined&account_id=...`.
    // `kind` is never validated server-side, so the literal string
    // "undefined" would become a real filter matching nothing -- a silently
    // empty ledger indistinguishable from end-of-history. `account_id`
    // fares differently but no better: "undefined" is not empty, so it skips
    // parseTransactionFilter's absent-filter check and fails isValidUUID
    // instead, turning a UI bug into a 422 INVALID_ACCOUNT_FILTER.
    expect(toQueryString({ month: "2026-07" })).toBe("?month=2026-07");
  });

  // "" and undefined are different filters, and only the server can tell
  // them apart from what arrives. An absent month means "no preference", which
  // parseTransactionFilter answers for the current month; the household
  // asking for every month has to say so, and `month=all` is how. Sent as an
  // absent parameter instead, the widening would silently resolve back to the
  // current month while the control showed itself cleared.
  it("sends a deliberately cleared month as month=all", () => {
    expect(toQueryString({ month: "" })).toBe("?month=all");
  });

  it("sends no month at all when none was chosen", () => {
    expect(toQueryString({ kind: "expense" })).toBe("?kind=expense");
  });

  it("maps every filter to the server's snake_case query param", () => {
    const query = toQueryString({
      kind: "expense",
      accountId: "acc-1",
      categoryId: "cat-1",
      paidBy: "member-1",
      month: "2026-07",
      cursor: "2026-07-01:tx-1",
    });
    const params = new URLSearchParams(query);
    expect(params.get("kind")).toBe("expense");
    expect(params.get("account_id")).toBe("acc-1");
    expect(params.get("category_id")).toBe("cat-1");
    expect(params.get("paid_by")).toBe("member-1");
    expect(params.get("month")).toBe("2026-07");
    expect(params.get("cursor")).toBe("2026-07-01:tx-1");
  });
});
