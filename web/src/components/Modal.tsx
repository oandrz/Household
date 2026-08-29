// The shared modal primitive. Lives in components/, not a feature folder --
// slices 2-4 build roughly fifteen modals on it, and moving a shared
// primitive once it has fifteen call sites is expensive.
//
// Built on the native <dialog> element so focus trapping and Escape handling
// come from the platform rather than hand-written key handling: a real
// browser's showModal() puts the dialog in the top layer, traps Tab focus
// inside it, and fires a "cancel" event (which this listens for via
// onCancel) when the user presses Escape.
//
// jsdom does not implement any of that -- its HTMLDialogElement is a five-
// line stub with no showModal(), no close(), and consequently no
// "cancel"/"close" events (see
// node_modules/jsdom/lib/jsdom/living/nodes/HTMLDialogElement-impl.js).
// Every call to those two methods below is feature-detected rather than
// assumed, purely so this renders without throwing under jsdom; skipping
// them there does not change anything a real, capable browser does, since a
// browser that has them always takes this branch.
//
// Backdrop-vs-panel detection and the ✕ button do not depend on showModal()
// at all -- they're ordinary DOM click handling and are unaffected by
// jsdom's gap. Focus entering the dialog on open and returning to the
// trigger on close *is* this component's own logic (real browsers move
// focus into the dialog automatically on showModal(), and restore it to the
// previously-focused element on close, per the dialog close steps in the
// HTML spec -- but jsdom does neither, so this implements both explicitly,
// stepping aside for the platform's own choice when one already exists).
//
// Fix round 1, Finding 1: this used to render the `open` attribute
// declaratively on every render (`<dialog open ...>`) and then call
// `showModal()` from an effect. In a real browser that throws --
// showModal()'s first spec'd step is "if this has an open attribute, throw
// an InvalidStateError" -- so React committing the attribute before the
// effect ever runs made every open() a guaranteed exception in production.
// jsdom hid this completely: its HTMLDialogElement has no showModal() at
// all, so the feature-detection guard always took the no-op branch and the
// throwing one was never reached in any test. `supportsShowModal` is
// computed once, at module scope, specifically so the two paths never mix
// for a single element: a capable engine never gets the attribute rendered
// declaratively (showModal() sets it itself, and close() clears it), and
// only an engine without showModal() (this file's only reason to exist:
// jsdom, or a hypothetical non-modal-capable browser) gets `open` set by
// JSX at all.
import { type ReactNode, useEffect, useId, useRef } from "react";

const supportsShowModal =
  typeof HTMLDialogElement !== "undefined" &&
  typeof HTMLDialogElement.prototype.showModal === "function";

export function Modal({
  open,
  onClose,
  title,
  children,
  wide = false,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  // Opt-in second width, defaulting to the 420px every modal used to get.
  // The design does not give all modals one width: the Edit-vision modal is
  // drawn at 640px (dc.html:928) because it nests three levels of editable
  // rows -- pillars, each pillar's measures, then milestones -- and at 420px
  // those nested cards and their remove buttons crowd each other. A boolean
  // rather than a number: both values are static classes Tailwind can see at
  // build time, which a runtime `w-[${n}px]` would not be, and the call site
  // reads as intent ("this is the wide one") rather than a magic number.
  wide?: boolean;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);
  const titleId = useId();

  useEffect(() => {
    if (!open) return;

    previouslyFocusedRef.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;

    const dialog = dialogRef.current;
    if (dialog && supportsShowModal) {
      dialog.showModal();
    }

    // showModal() focuses the first focusable descendant, which here is the
    // header's ✕ -- the header precedes the children -- so a keyboard user
    // opening a form modal started on Close rather than on the first thing to
    // fill in. Prefer the panel's first form control, and fall back to the
    // platform's own choice when the panel has none, so a confirmation modal
    // with only buttons still lands inside the dialog rather than leaving
    // focus wherever the page had it.
    const firstField = dialog?.querySelector<HTMLElement>(
      "input:not([type=hidden]), select, textarea",
    );
    if (firstField) {
      firstField.focus();
    } else if (dialog && !dialog.contains(document.activeElement)) {
      dialog.focus();
    }

    return () => {
      if (dialog && supportsShowModal && dialog.open) {
        dialog.close();
      }
      const previouslyFocused = previouslyFocusedRef.current;
      if (previouslyFocused && document.contains(previouslyFocused)) {
        previouslyFocused.focus();
      }
    };
  }, [open]);

  if (!open) return null;

  return (
    <dialog
      ref={dialogRef}
      // Only set declaratively on the fallback path -- a capable engine's
      // showModal()/close() are the sole managers of this attribute (see the
      // file header comment). jsdom (this repo's test environment) always
      // takes this branch, since it has no showModal() at all.
      {...(!supportsShowModal ? { open: true } : {})}
      tabIndex={-1}
      aria-labelledby={titleId}
      // w-screen/h-dvh matter, not just cosmetically: <dialog>'s default
      // UA sizing is width/height: fit-content, which -- confirmed in a real
      // browser (Chromium), not inferred -- wins over `inset-0` stretching
      // the box to the viewport. Without an explicit full-viewport size, the
      // dialog's own box shrink-wraps its single child exactly, leaving no
      // backdrop area at all for a click-outside-to-dismiss to ever land on;
      // getBoundingClientRect() on the dialog and its panel were identical
      // (420x118, both) before this was added.
      // h-dvh, not h-screen: on iOS Safari `100vh` is the *large* viewport --
      // the height with the URL bar hidden -- so a dialog sized to it puts its
      // bottom edge under the browser toolbar. AccountModal's content is 665px
      // against roughly 650px of visible height on an iPhone, which is its
      // submit button sitting exactly where the user cannot reach it.
      className="m-0 h-dvh w-screen max-h-none max-w-none border-none bg-transparent p-0 open:fixed open:inset-0 open:grid open:place-items-center open:bg-black/40"
      onCancel={(event) => {
        // Fired by a real browser when the user presses Escape on a modal
        // dialog. preventDefault stops the platform's own close (which
        // would leave `open`, this component's own prop, out of sync) --
        // onClose is the single source of truth for whether the dialog is
        // open, driven back through the caller's state.
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        // The dialog element's own box covers the backdrop; a click that
        // lands there (not on the panel inside it, which stops propagation
        // by virtue of being a distinct element the event bubbles up
        // from) has event.target === event.currentTarget.
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className={`max-w-[calc(100vw-32px)] ${wide ? "w-[640px]" : "w-[420px]"} rounded-2xl border border-hairline bg-card p-6 shadow-[var(--shadow-auth-card)]`}
      >
        <div className="mb-4 flex items-center justify-between gap-4">
          <h2
            id={titleId}
            className="font-serif text-xl font-medium tracking-[-0.01em]"
          >
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            // 44px floor on phones, restoring at `sm`: modals aren't tied to
            // the shell's `lg` nav switch, so the pointer breakpoint is fine.
            className="grid h-11 w-11 flex-none place-items-center rounded-lg bg-canvas text-[13px] text-label sm:h-7 sm:w-7"
          >
            ✕
          </button>
        </div>
        {children}
      </div>
    </dialog>
  );
}
