import { describe, expect, it } from "vitest";
import { formatMoney, toMinorUnits } from "./formatMoney";

describe("formatMoney", () => {
  it("renders the design's SGD figure", () => {
    expect(formatMoney(824055, "SGD", "S$")).toBe("S$8,240.55");
  });

  it("renders a debt with its minus sign outside the symbol", () => {
    expect(formatMoney(-1450000, "SGD", "S$")).toBe("−S$14,500.00");
  });

  // IDR is a two-minor-unit currency in the allowlist, but the design draws
  // Rp 85,400,000 with no decimals -- nobody quotes rupiah cents. This is a
  // display choice; the stored value keeps its minor units.
  it("renders IDR without decimals, as the design does", () => {
    expect(formatMoney(8540000000, "IDR", "Rp")).toBe("Rp 85,400,000");
  });

  it("falls back to the bare code when no symbol is known", () => {
    expect(formatMoney(100000, "BRL")).toBe("BRL 1,000.00");
  });
});

describe("toMinorUnits", () => {
  it("parses a figure whose cents would be wrong as a float", () => {
    // 8240.55 * 100 is 824054.9999999999 in IEEE 754.
    expect(toMinorUnits("8240.55", "SGD")).toBe(824055);
    expect(toMinorUnits("0.29", "SGD")).toBe(29);
  });

  // The pair that catches a scale confusion: minor units are hundredths for
  // every currency this app accepts, including IDR. Only the number of digits
  // a person may type differs.
  it("round-trips the design's IDR figure through both directions", () => {
    expect(toMinorUnits("85400000", "IDR")).toBe(8540000000);
    expect(formatMoney(8540000000, "IDR", "Rp")).toBe("Rp 85,400,000");
  });

  it("refuses more precision than the field offers", () => {
    expect(toMinorUnits("8240.555", "SGD")).toBeNull();
    expect(toMinorUnits("85400000.5", "IDR")).toBeNull();
    expect(toMinorUnits("eight", "SGD")).toBeNull();
  });
});
