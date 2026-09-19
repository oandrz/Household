// The retro modal's add-action composer -- a body input, one toggle per
// household owner (none, one or both may be selected; RetroActionRow.tsx's
// own rule already renders zero assignees as no initials rather than a
// placeholder, so nothing here forces a choice), and an Add control. Pulled
// out of RetroModal.tsx; its state, handlers and comments moved here
// unchanged.
//
// The composer is a fix-round addition, not part of Task 14's own tests: the
// modal shipped with the carry-over offer as its actions block's only caller
// of `addAction`, which meant a household could carry last month's
// unfinished action forward but never write a brand-new one -- the exact
// shape docs/LEARNING.md pattern 15 names (a feature fully built and tested
// one layer down with no screen that can reach it). Fixed then rather than
// left for Task 17's browser walk to rediscover.
//
// `data-testid="retro-add-action"` pins this block's own wiring: an assertion
// that finds this testid, or the input/Add button inside it, would go red if
// this block's render were ever deleted from the modal -- the same "prove the
// parent still renders the child" shape Tasks 11-13 each had to add after
// review.
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import type { MemberView } from "../settings/schemas";
import { RETRO_COPY } from "./retroCopy";
import type { useRetro } from "./useRetro";

export function AddActionComposer({
  members,
  addAction,
  disabled,
}: {
  // Every household member, straight from RetroModal's useHouseholdMembers()
  // -- filtered to owners below.
  members: MemberView[];
  addAction: ReturnType<typeof useRetro>["addAction"];
  // useRetroDraft's `actionsDisabled`: a save in flight, or the conflict latch.
  disabled: boolean;
}) {
  // The composer's own local state -- a brand-new action, never a carry (see
  // handleAddAction's own comment on why `carriedFrom` is never sent here).
  // Reset to blank after a successful post so the household can add a second
  // action right away without reopening anything -- the exact flow Task 17's
  // browser-walk criterion 7 exercises ("Add two actions, assign one to each
  // partner and one to both").
  const [newActionBody, setNewActionBody] = useState("");
  const [newActionAssigneeIds, setNewActionAssigneeIds] = useState<Set<string>>(new Set());
  const [isAddingAction, setIsAddingAction] = useState(false);
  const [addActionError, setAddActionError] = useState<string | null>(null);

  // The household's owners, in the order useHouseholdMembers itself returns
  // them -- filtered here rather than a second fetch (useHouseholdMembers.ts's
  // own header comment: the point of that hook is one shared cache entry).
  // `member.role` is a plain string on the wire (memberSchema's own
  // `z.string()`, not an enum) -- "owner" is the same literal
  // AccountModal.tsx/MembersPanel.tsx already filter and compare against.
  const owners = members.filter((member) => member.role === "owner");

  function toggleAssignee(memberId: string) {
    setNewActionAssigneeIds((prev) => {
      const next = new Set(prev);
      if (next.has(memberId)) {
        next.delete(memberId);
      } else {
        next.add(memberId);
      }
      return next;
    });
  }

  // Posts a brand-new action -- deliberately no `carriedFrom` at all, not
  // even `""`: useRetro.ts's own AddRetroActionBody defaults an omitted
  // `carriedFrom` to `""` on the wire itself, so this function has nothing
  // to pass either way, and never should. `carriedFrom` is CarryOverList's
  // field alone (its handleCarryOver comment states the trust boundary: only
  // ever an id taken from `retro.data.carryOver`), and a composer that also
  // set it would be exactly the freehand-id risk that comment warns against.
  //
  // The Add control's own `disabled` already keeps this unreachable with a
  // blank body (see the button below), but the check is repeated here for
  // the same reason useRetroDraft's `finish` repeats `actionsDisabled` -- an
  // invariant this function owns, not one left incidental to a button's own
  // attribute.
  async function handleAddAction() {
    const body = newActionBody.trim();
    if (body === "" || disabled) return;
    setAddActionError(null);
    setIsAddingAction(true);
    try {
      await addAction({ body, assigneeMembershipIds: Array.from(newActionAssigneeIds) });
      setNewActionBody("");
      setNewActionAssigneeIds(new Set());
    } catch (err) {
      setAddActionError(apiErrorMessage(err, RETRO_COPY.addActionError));
    } finally {
      setIsAddingAction(false);
    }
  }

  return (
    <div
      data-testid="retro-add-action"
      className="flex flex-col gap-2 rounded-[10px] border border-dashed border-hairline p-3.5"
    >
      <input
        type="text"
        value={newActionBody}
        onChange={(event) => setNewActionBody(event.target.value)}
        // Enter adds the action, which is what someone typing one
        // means by it. handleAddAction re-checks the same conditions
        // the Add button's `disabled` carries (blank body, conflict
        // latch), so this cannot do anything the button would refuse.
        // `isAddingAction` is checked here and nowhere else in
        // handleAddAction: the Add button gets it through its own
        // `disabled`, but a key repeat does not, so without it a fast
        // double-press posts the action twice while the first request
        // is still in flight. A duplicate matters more here than it
        // sounds -- there is no delete-action control (deliberately;
        // the design draws none), so the household cannot remove one.
        onKeyDown={(event) => {
          if (event.key !== "Enter" || isAddingAction) return;
          event.preventDefault();
          void handleAddAction();
        }}
        placeholder={RETRO_COPY.addActionPlaceholder}
        disabled={disabled}
        className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13px] sm:min-h-0"
      />
      <div className="flex flex-wrap items-center gap-2">
        {owners.map((owner) => {
          const selected = newActionAssigneeIds.has(owner.id);
          return (
            <button
              key={owner.id}
              type="button"
              aria-label={RETRO_COPY.assignToMember(owner.user.displayName)}
              aria-pressed={selected}
              disabled={disabled}
              onClick={() => toggleAssignee(owner.id)}
              // Review finding: a hard `sm:h-[26px] sm:w-[26px]`
              // clamp shrank this below the 44px floor on every
              // viewport >=640px, including desktop, and was never
              // measured or named as an exception -- the only
              // `sm:h-[Npx]` on an interactive element anywhere in
              // features/marriage, features/money or components.
              // `min-h-11 ... sm:min-h-0` is the house pattern every
              // other control here uses (RetroActionRow.tsx's own
              // checkbox label, every button in the retro modal): it
              // removes the floor rather than clamping below it, so
              // padding decides the size at `sm` the same way it
              // does everywhere else. `aspect-square` keeps this
              // circular at both sizes without a second, separate
              // width utility to keep in sync with the height one.
              className={`flex aspect-square min-h-11 flex-none items-center justify-center rounded-full border p-1.5 text-[13px] font-semibold disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 ${
                selected ? "border-accent bg-callout text-accent" : "border-hairline text-label"
              }`}
            >
              {owner.user.avatarInitial}
            </button>
          );
        })}
        <button
          type="button"
          disabled={disabled || isAddingAction || newActionBody.trim() === ""}
          onClick={() => void handleAddAction()}
          className="min-h-11 ml-auto flex-none rounded-lg bg-accent px-4 text-[12px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
        >
          {RETRO_COPY.addAction}
        </button>
      </div>
      {addActionError && (
        <p role="alert" className="text-xs leading-snug text-danger">
          {addActionError}
        </p>
      )}
    </div>
  );
}
