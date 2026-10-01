import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { BrandMark } from "./BrandMark";

describe("BrandMark", () => {
  it("draws the letter H on the brand square", () => {
    const { container } = render(<BrandMark size="shell" />);

    expect(container.firstElementChild).toHaveTextContent("H");
    expect(container.firstElementChild).toHaveClass("bg-accent");
  });

  // The wordmark "Hearth" always sits beside it. Read aloud, an exposed mark
  // would make that "H Hearth".
  it("is hidden from screen readers, so the wordmark beside it is not read twice", () => {
    const { container } = render(<BrandMark size="auth" />);

    expect(container.firstElementChild).toHaveAttribute("aria-hidden", "true");
  });
});
