// The hamburger is the only way back to navigation below lg, so its
// accessible name is load-bearing: a screen-reader user with no visible
// sidebar has nothing else to go on.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MobileTopBar } from "./MobileTopBar";

describe("MobileTopBar", () => {
  it("exposes the nav trigger by an accessible name", () => {
    render(<MobileTopBar onOpenNav={() => {}} />);

    expect(screen.getByRole("button", { name: "Open navigation" })).toBeInTheDocument();
  });

  it("calls onOpenNav when the trigger is pressed", () => {
    const onOpenNav = vi.fn();
    render(<MobileTopBar onOpenNav={onOpenNav} />);

    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));

    expect(onOpenNav).toHaveBeenCalledTimes(1);
  });
});
