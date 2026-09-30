import { describe, expect, it } from "vitest";
import { BILL_COPY } from "./billCopy";

describe("BILL_COPY.rowSubtitle", () => {
  it("joins category, payment mode and account with separators", () => {
    expect(BILL_COPY.rowSubtitle("Utilities", false, "DBS")).toBe("Utilities · manual · DBS");
  });

  it("does not open with a separator when the bill has no category", () => {
    expect(BILL_COPY.rowSubtitle("", true, "DBS")).toBe("autopay · DBS");
  });
});
