// One action row inside a retro's detail -- the design's checkbox, body
// text, carried-from provenance and per-assignee initial circles
// (dc.html's "Phone-free dinners on weekdays  A C" row).
//
// The tick is a real `<input type="checkbox">` with a `<label htmlFor>`
// naming it, NOT an `sr-only` input behind a styled stand-in div. That
// exact shape shipped keyboard-invisible focus in TransactionsPage's Kind
// filter (docs/LEARNING.md pattern 3): every radio was `sr-only`, the
// visible pill never reacted to the hidden input's own `:focus-visible`, and
// no unit test caught it because `fireEvent.click` fires straight at the
// element rather than pressing Tab or an arrow key the way a real keyboard
// user does. A real, visible checkbox has no such gap to open in the first
// place -- Tab lands on it, the browser draws its own focus ring, nothing
// here has to reimplement that.
import { RETRO_COPY, previousMonthName } from "./retroCopy";
import type { RetroAction } from "./retroSchemas";
import type { MemberView } from "../settings/schemas";

export function RetroActionRow({
  action,
  members,
  retroMonth,
  pending,
  onToggle,
}: {
  action: RetroAction;
  members: MemberView[];
  retroMonth: string;
  pending: boolean;
  onToggle: (actionId: string, done: boolean) => void;
}) {
  const done = action.doneAt !== null;
  const checkboxId = `retro-action-checkbox-${action.id}`;

  return (
    <div
      data-testid={`retro-action-${action.id}`}
      className="flex items-center justify-between gap-3 rounded-[10px] border border-hairline px-3.5 py-2.5"
    >
      <div className="flex min-w-0 flex-col">
        {/* The label uses `flex` only to get `items-center` (vertically
            centering the checkbox against the text) -- it does NOT reach
            into the row's remaining width the way docs/LEARNING.md's own
            min-height-on-inline entry warns a bare `flex` element can. That
            entry's mechanism does not apply here: this label is already a
            flex ITEM of the row above, and a flex item's own `display`
            value (inline-flex or flex, no difference once it's an item) is
            blockified by its parent's layout regardless -- what actually
            keeps this label from growing into the assignee circles' space
            is the OUTER row below being `justify-between` with neither
            child given `flex-grow`, so both size to their own content and
            the free space becomes the gap between them, not extra width on
            either side.
            min-h-11 on the label is the 44px touch-target floor (CLAUDE.md)
            on the one genuinely interactive control here -- native
            `<label for>` click-delegation gives the 18x18px checkbox a
            44px-tall tappable strip without visually enlarging the glyph
            itself, dropping to the design's own tighter spacing at sm and
            up (min-h-11/sm:min-h-0, the same pattern every other control on
            this screen uses). See this task's own report for why the glyph
            stays 18px rather than growing to 44px outright. */}
        <label htmlFor={checkboxId} className="flex min-h-11 cursor-pointer items-center gap-2.5 sm:min-h-0">
          <input
            id={checkboxId}
            type="checkbox"
            checked={done}
            disabled={pending}
            onChange={(event) => onToggle(action.id, event.target.checked)}
            className="h-[18px] w-[18px] flex-none rounded border-2 border-hairline accent-accent disabled:cursor-not-allowed"
          />
          <span className={`text-[13px] ${done ? "text-muted line-through" : "text-ink"}`}>{action.body}</span>
        </label>
        {/* carriedFrom is "" for an action nobody carried (retroActionSchema's
            own comment) -- guarded rather than rendered unconditionally with
            an empty label, the same "no placeholder for an absence" rule
            this screen already follows for the history row's quote/action
            count clauses. */}
        {action.carriedFrom !== "" && (
          <p className="ml-[26px] mt-0.5 text-[11px] text-muted">
            {RETRO_COPY.carriedFrom(previousMonthName(retroMonth))}
          </p>
        )}
      </div>
      {/* One initial per assignee, none at all when there are none -- never
          a placeholder circle for "nobody yet" (task-12-brief.md's own
          design-details paragraph). A membership id with no matching member
          (a removed household member whose old assignment is still on
          record) renders nothing for that id rather than a blank circle --
          failing closed on a value this screen did not itself construct,
          the same rule CLAUDE.md states for a value from a database column
          or a request. */}
      <div className="flex flex-none gap-1.5">
        {action.assigneeMembershipIds.map((membershipId) => {
          const member = members.find((m) => m.id === membershipId);
          if (!member) return null;
          return (
            <span
              key={membershipId}
              title={member.user.displayName}
              className="flex h-[22px] w-[22px] items-center justify-center rounded-full bg-callout text-[9.5px] font-semibold text-accent"
            >
              {member.user.avatarInitial}
            </span>
          );
        })}
      </div>
    </div>
  );
}
