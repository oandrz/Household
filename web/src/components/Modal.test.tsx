// Behaviours from the task-19 brief:
// 1. open={false} renders nothing.
// 2. open renders the title and children.
// 3. Escape calls onClose.
// 4. Clicking the backdrop calls onClose; clicking inside the panel does not.
// 5. Focus moves into the modal on open and returns to the trigger on close.
//
// jsdom 29's HTMLDialogElement implements only the `open` attribute
// reflection -- no showModal(), no close(), no "cancel"/"close" events (see
// node_modules/jsdom/lib/jsdom/living/nodes/HTMLDialogElement-impl.js, which
// is a five-line stub). Modal.tsx therefore feature-detects both methods
// before calling them. That also means behaviour 3 cannot be exercised as
// "press Escape, the browser fires cancel, onClose runs" in this
// environment -- there is no keyboard-to-event wiring here at all, native or
// otherwise. Test 3 below dispatches the "cancel" event directly (what a
// real browser's Escape handling fires on a modal <dialog>) to verify
// Modal's own handler is attached and calls onClose; it does not verify that
// a physical Escape keypress produces that event, which only a real browser
// can do. Test 5 verifies the focus-management Modal implements itself in
// JS, not a platform behaviour, which is called out as needed regardless of
// jsdom in the component's own comments.
import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "./Modal";

function OpenCloseHarness() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button onClick={() => setOpen(true)}>Open trigger</button>
      <Modal open={open} onClose={() => setOpen(false)} title="Example modal">
        <button>Panel button</button>
      </Modal>
    </div>
  );
}

describe("Modal", () => {
  it("renders nothing when closed", () => {
    render(
      <Modal open={false} onClose={() => {}} title="Hidden title">
        <p>Hidden content</p>
      </Modal>,
    );

    expect(screen.queryByText("Hidden title")).not.toBeInTheDocument();
    expect(screen.queryByText("Hidden content")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("renders the title and children when open", () => {
    render(
      <Modal open onClose={() => {}} title="Example modal">
        <p>Hello from inside the modal</p>
      </Modal>,
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Example modal")).toBeInTheDocument();
    expect(screen.getByText("Hello from inside the modal")).toBeInTheDocument();
  });

  it("calls onClose when the dialog's cancel event fires", () => {
    const onClose = vi.fn();
    render(
      <Modal open onClose={onClose} title="Example modal">
        <p>Body</p>
      </Modal>,
    );

    fireEvent(screen.getByRole("dialog"), new Event("cancel", { cancelable: true }));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose when the backdrop is clicked, but not when the panel is clicked", () => {
    const onClose = vi.fn();
    render(
      <Modal open onClose={onClose} title="Example modal">
        <button>Panel button</button>
      </Modal>,
    );

    fireEvent.click(screen.getByText("Panel button"));
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("dialog"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("moves focus into the modal on open and returns it to the trigger on close", () => {
    render(<OpenCloseHarness />);

    const trigger = screen.getByText("Open trigger");
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    fireEvent.click(trigger);
    const dialog = screen.getByRole("dialog");
    expect(dialog.contains(document.activeElement)).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: /close/i }));
    expect(document.activeElement).toBe(trigger);
  });

  // A real browser's showModal() lands focus on the first focusable
  // descendant, which is the header's ✕ -- the header precedes the form -- so
  // opening "Log a transaction" from the keyboard started the user on Close.
  // Measured live before the fix; jsdom cannot reproduce it, because it has no
  // showModal() and so never moves focus at all, which is exactly why this
  // test can only prove the explicit choice below, never the platform's.
  it("lands focus on the first form field, not the close button", () => {
    render(
      <Modal open onClose={() => {}} title="Log a transaction">
        <form>
          <label htmlFor="amount">Amount</label>
          <input id="amount" />
        </form>
      </Modal>,
    );

    expect(document.activeElement).toBe(screen.getByLabelText("Amount"));
  });

  // A modal with nothing to fill in must still land somewhere inside itself,
  // or Tab would resume from wherever the page was and walk straight out of a
  // dialog the platform has not trapped.
  it("still lands inside a modal that has no form field", () => {
    render(
      <Modal open onClose={() => {}} title="Delete this?">
        <button type="button">Yes, delete</button>
      </Modal>,
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog.contains(document.activeElement)).toBe(true);
  });
});
