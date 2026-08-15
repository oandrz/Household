// The drawer is deliberately NOT built on <dialog>, unlike components/Modal
// -- the same element has to be the static desktop column, and a <dialog>
// cannot be one (see NavDrawer.tsx's header comment). That trade means the
// three things <dialog> would have supplied for free are hand-written here,
// so all three are tested: Escape closes, a backdrop press closes, and focus
// goes into the drawer on open and back to the trigger on close.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NavDrawer } from "./NavDrawer";

function Harness({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <>
      <button type="button">Open navigation</button>
      <NavDrawer open={open} onClose={onClose}>
        <nav>
          <a href="/">Overview</a>
        </nav>
      </NavDrawer>
    </>
  );
}

describe("NavDrawer", () => {
  it("renders its children", () => {
    render(<Harness open={false} onClose={() => {}} />);

    expect(screen.getByText("Overview")).toBeInTheDocument();
  });

  it("closes on Escape while open", () => {
    const onClose = vi.fn();
    render(<Harness open onClose={onClose} />);

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ignores Escape while closed, so it cannot close a drawer nobody opened", () => {
    const onClose = vi.fn();
    render(<Harness open={false} onClose={onClose} />);

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes when the backdrop is pressed", () => {
    const onClose = vi.fn();
    render(<Harness open onClose={onClose} />);

    fireEvent.click(screen.getByTestId("nav-drawer-backdrop"));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("has no backdrop while closed", () => {
    render(<Harness open={false} onClose={() => {}} />);

    expect(screen.queryByTestId("nav-drawer-backdrop")).toBeNull();
  });

  it("moves focus into the drawer on open and returns it on close", () => {
    const trigger = () => screen.getByRole("button", { name: "Open navigation" });
    const { rerender } = render(<Harness open={false} onClose={() => {}} />);

    trigger().focus();
    expect(document.activeElement).toBe(trigger());

    rerender(<Harness open onClose={() => {}} />);
    expect(screen.getByTestId("nav-drawer").contains(document.activeElement)).toBe(true);

    rerender(<Harness open={false} onClose={() => {}} />);
    expect(document.activeElement).toBe(trigger());
  });

  // Task 3 passes an inline arrow (`onClose={() => setNavOpen(false)}`), a
  // fresh function identity on every render of its parent. If the
  // focus-management effect depended on that identity, an unrelated parent
  // re-render while the drawer is open would tear the effect down and set it
  // back up, yanking focus off whatever the user had tabbed to inside the
  // drawer and back onto the panel.
  it("does not move focus when a re-render supplies a new onClose identity", () => {
    const { rerender } = render(<Harness open onClose={() => {}} />);

    const link = screen.getByRole("link", { name: "Overview" });
    link.focus();
    expect(document.activeElement).toBe(link);

    rerender(<Harness open onClose={() => {}} />);

    expect(document.activeElement).toBe(link);
  });
});
