// The Telegram half of one pending-invite row: everything an owner sees
// while a partner is, in the same room, picking up their phone to tap a
// link. Mounted once per Telegram invite in PendingInvitesList.tsx, and
// again (Task 12) inside InviteMemberModal's success state -- which is why
// this component owns every mutation it needs (Withdraw, a new link, Admit)
// rather than taking them as callback props: `link`, `onNewLink` and
// `onAdmitted` are its only optional inputs, because the modal is the only
// caller that still holds a link fresh from a 201 and needs to hear about a
// replacement, and PendingInvitesList is the only caller whose observed
// query can remove this card's own row out from under it (see `onAdmitted`
// below).
//
// Four states, decided in this order:
//   0. useAdmitInvite().data truthy -> admitted (see below for why this one
//      is not read off `invite`)
//   1. invite.knock present            -> knocked
//   2. no knock, this session holds a link -> waiting, link in hand
//   3. no knock, no link held               -> waiting, link not in hand
// State 1 (knocked) is read off `invite` itself, never off a flag this
// component invents, so two tabs looking at the same invite agree on it.
// States 2 and 3 are NOT read off `invite` -- there is nothing on the wire
// to read; a link exists only in the hand of whichever session minted it.
// They split on `effectiveLink`, which is session-local *by design*: either
// the `link` prop (a fresh 201 the modal just received) or `heldLink` (a
// link this same card minted itself via "Not them"/"Get a new link", set
// below). Two tabs are expected to differ here -- one may hold a link the
// other never asked for -- while still agreeing on state 1. Do not read
// this as contradicting "read off `invite`": it says which *tab-local*
// state states 2/3 read, not that they secretly read the row.
//
// State 0 (admitted) is the deliberate exception to state 1's rule, and for
// a different reason than 2/3: it is read from useAdmitInvite().data
// because there is no invite left at all to read it from. Admit stamps the
// invite accepted, and pendingInvitesQueryKey (one of the three keys
// usePendingInvites.ts's useAdmitInvite invalidates) means the row this
// `invite` prop describes leaves the pending list on the very next refetch.
// By the time that refetch lands there is no accepted invite left to read
// "admitted" from -- the mutation's own result is the only place it still
// lives. Do not "fix" the asymmetry by moving state 1 onto a local flag
// too; it stays read off `invite` on purpose.
//
// Because state 0 lives only as long as this card does, and this card can
// be unmounted the moment the refetch above removes its row,
// PendingInvitesList cannot rely on this card to show the admitted notice
// for long -- see `onAdmitted` below and PendingInvitesList.tsx's own
// admittedResults state for how it survives that.
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { useConfirmAction } from "../../components/useConfirmAction";
import { admittedLine, knockLine, pendingInviteExpiryLine } from "./copy";
import { InviteLinkShare } from "./InviteLinkShare";
import type { AdmitResult, PendingInvite } from "./schemas";
import { useAdmitInvite, useNewInviteLink, useWithdrawInvite } from "./usePendingInvites";

const CARD_CLASS = "flex flex-col gap-3 rounded-xl border border-hairline bg-card p-4";
const PRIMARY_BUTTON_CLASS =
  "min-h-11 rounded-lg bg-accent px-3 py-1.5 text-[12px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const SECONDARY_BUTTON_CLASS =
  "min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[12px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";

// Withdraw, in-page confirm first -- the same shape as NewLinkControl just
// below (trigger, or the confirm line and its pair, never both), and the
// same hook ApiTokenList's Revoke and TelegramConnection's Disconnect use.
// Self-contained like NewLinkControl for the same reason: PendingInviteCard
// is mounted once per invite, so this owns its own mutation and confirm
// state rather than taking them as props.
function WithdrawControl({ id, name }: { id: string; name: string }) {
  const withdraw = useWithdrawInvite();
  const action = useConfirmAction("Couldn't withdraw that invite. Please try again.");
  const error = action.errorFor();

  return (
    <>
      {!action.isConfirming() && (
        <button
          type="button"
          onClick={() => action.ask()}
          disabled={action.isPending()}
          aria-label={`Withdraw the invite to ${name}`}
          // min-h-11/sm:min-h-0: PendingInvitesList.tsx's own Withdraw button
          // has the measured reason a control this small misses the 44px phone
          // floor.
          className="min-h-11 flex-none text-xs font-semibold text-danger disabled:opacity-50 sm:min-h-0"
        >
          Withdraw
        </button>
      )}
      {action.isConfirming() && (
        <div className="flex flex-col gap-2">
          <p className="text-[12px] text-muted">Withdraw this invite? The link stops working.</p>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void action.confirm(() => withdraw.mutateAsync(id))}
              disabled={action.isPending()}
              className="min-h-11 rounded-lg bg-danger px-3 py-1.5 text-[11px] font-semibold text-white disabled:opacity-60 sm:min-h-0"
            >
              Yes, withdraw
            </button>
            <button
              type="button"
              onClick={() => action.cancel()}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
            >
              Keep
            </button>
          </div>
        </div>
      )}
      {/* Read outside both branches, like ApiTokenList's and
          TelegramConnection's own `error` line: useConfirmAction's `confirm`
          collapses the pair back to the trigger on every outcome, so a
          message shown only inside the confirming branch above would vanish
          with it. This is what keeps it on screen afterwards. */}
      {error && (
        <p role="alert" className="text-[11px] text-danger">
          {error}
        </p>
      )}
    </>
  );
}

// "Not them" (knocked state) and "Get a new link" (either waiting state)
// are the SAME mutation -- both call useNewInviteLink, which POSTs the one
// endpoint behind it (invite_lobby_handlers.go's handleNewInviteLink): it
// clears any knock, mints a fresh one-time link and kills the previous one.
// Never two states' controls on screen at once, so never two mounted
// instances either -- the three call sites below (one per state that shows
// this control) differ only in this component's `triggerLabel` and
// `confirmBody` props, never in what gets sent -- look here first, not for
// a second route, if a future change seems to need one.
//
// The confirm step is in-page (useConfirmAction), never window.confirm: a
// native dialog blocks the extension this project's browser walks run on,
// and it can't be styled or asserted against in a test either
// (DiscardDraftControl.tsx's own comment gives the same reason).
function NewLinkControl({
  id,
  triggerLabel,
  confirmBody,
  onNewLink,
}: {
  id: string;
  triggerLabel: string;
  confirmBody: string;
  onNewLink?: (link: string) => void;
}) {
  const newLink = useNewInviteLink();
  const action = useConfirmAction("Couldn't get a new link. Please try again.");

  function handleConfirm() {
    void action.confirm(async () => {
      const result = await newLink.mutateAsync(id);
      onNewLink?.(result.link);
    });
  }

  if (action.isConfirming()) {
    return (
      <div className="flex flex-col gap-2">
        <p className="text-[12px] text-muted">{confirmBody}</p>
        <div className="flex gap-2">
          <button type="button" onClick={action.cancel} className={SECONDARY_BUTTON_CLASS}>
            Cancel
          </button>
          <button
            type="button"
            disabled={action.isPending()}
            onClick={handleConfirm}
            className={PRIMARY_BUTTON_CLASS}
          >
            {triggerLabel}
          </button>
        </div>
        {action.errorFor() && (
          <p role="alert" className="text-[11px] text-danger">
            {action.errorFor()}
          </p>
        )}
      </div>
    );
  }

  return (
    <button type="button" onClick={() => action.ask()} className={SECONDARY_BUTTON_CLASS}>
      {triggerLabel}
    </button>
  );
}

export function PendingInviteCard({
  invite,
  link,
  onNewLink,
  onAdmitted,
}: {
  invite: PendingInvite;
  // The raw link, only while this session still holds it -- a link exists
  // only in the hand of the tab that just minted it (POST .../invite or
  // POST .../link), never persisted anywhere this component could read it
  // back from later.
  link?: string;
  // Called with a freshly minted link so a caller that still cares about
  // holding one (the modal, Task 12) can keep it. PendingInvitesList.tsx
  // passes nothing here: once a link is out of a session's hands, that
  // list has no further use for a new one beyond the refetch already
  // clearing the stale state.
  onNewLink?: (link: string) => void;
  // Called the moment Admit succeeds -- in the same tick `admit.data` lands,
  // not before it: query-core's Mutation#execute awaits the hook's own
  // onSettled (usePendingInvites.ts's three invalidations) *before*
  // dispatching success, and only that dispatch's onMutationUpdate both
  // sets `.data` on the observer and calls this per-call onSuccess (in that
  // order, via #notify). What actually matters is a different ordering:
  // #notify skips a per-call callback once the observer has no listeners,
  // i.e. once this card has unmounted -- which is exactly why
  // useAdmitInvite's onSettled fires its invalidations without awaiting
  // them (see that hook's own comment). A blocking onSettled would hold
  // the whole dispatch behind the very refetch that removes this card's
  // row, and onAdmitted would never fire at all. PendingInvitesList.tsx
  // uses this prop to hold the result itself, because *this card* can be
  // unmounted (its row removed by that same refetch) well before an owner
  // has had a chance to read a signInSent: false warning off it. The modal
  // (Task 12) does not pass this: it controls its own lifetime and keeps
  // showing this same card's own state-0 render instead.
  onAdmitted?: (result: AdmitResult) => void;
}) {
  const admit = useAdmitInvite();
  // A link this card's own "Not them"/"Get a new link" just minted. Without
  // this, clicking either button in state 1 or state 3 would POST a new
  // link, hand it to `onNewLink` -- which PendingInvitesList.tsx's call site
  // does not even pass -- and then have nowhere left to show it: the state
  // 2 branch below reads `link` (the prop, only ever set by a 201 the modal
  // just received) so a link minted *by this card* would otherwise vanish
  // into a callback nobody is listening to, and a second click would mint
  // another one into the same void. `link` (the prop) still wins on the
  // first render, so a modal handing this card a fresh 201's link is
  // unaffected; this only ever fills the gap after that.
  const [heldLink, setHeldLink] = useState<string | undefined>(undefined);
  const effectiveLink = heldLink ?? link;

  function handleNewLink(newLink: string) {
    setHeldLink(newLink);
    onNewLink?.(newLink);
  }

  // PendingInvitesList.tsx is the thing that decides whether to mount this
  // card or milestone-1's PendingInviteRow -- channel === "telegram" is the
  // switch, made once at that map callsite. This is a defensive backstop,
  // not the primary mechanism: without it, a future caller that skips that
  // switch would render Telegram-only controls (a QR code, a Let in button)
  // over an email invite that has no link, no knock and no /admit route
  // behind it at all.
  if (invite.channel !== "telegram") return null;

  const withdrawControl = <WithdrawControl id={invite.id} name={invite.name} />;

  // State 0 -- admitted. See the file header for why this is the one state
  // read from the mutation's own result rather than from `invite`.
  if (admit.data) {
    return (
      <li className={CARD_CLASS}>
        <p className="text-[13px] text-ink">{admittedLine(admit.data.signInSent)}</p>
      </li>
    );
  }

  // State 1 -- knocked.
  if (invite.knock) {
    const knock = invite.knock;
    return (
      <li className={CARD_CLASS}>
        <div>
          <p className="text-[13px] font-semibold text-ink">{knockLine(knock.username)}</p>
          <p className="mt-1 text-[13px] text-ink">
            Does their phone show <strong>{knock.code}</strong>?
          </p>
        </div>
        {admit.isError && (
          <p role="alert" className="text-[11px] text-danger">
            {apiErrorMessage(admit.error, "Couldn't let them in. Please try again.")}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2.5">
          <button
            type="button"
            disabled={admit.isPending}
            // The per-call onSuccess (not this hook's own onSettled) is
            // what reaches onAdmitted -- see that prop's own comment for
            // why the ordering matters.
            onClick={() => admit.mutate(invite.id, { onSuccess: (result) => onAdmitted?.(result) })}
            className={PRIMARY_BUTTON_CLASS}
          >
            Let in
          </button>
          <NewLinkControl
            id={invite.id}
            triggerLabel="Not them"
            confirmBody="This ends the tapped link. The phone that tapped it will see it no longer works, and you'll get a new link to share."
            onNewLink={handleNewLink}
          />
          {withdrawControl}
        </div>
      </li>
    );
  }

  // State 2 -- waiting, link in hand. `effectiveLink`, not the `link` prop
  // alone: a link this card just minted itself (heldLink, set above) counts
  // exactly as much as one handed down from a fresh 201.
  if (effectiveLink) {
    return (
      <li className={CARD_CLASS}>
        {/* Keyed on the link itself: InviteLinkShare's own "Copied" state
            must not survive a link swap -- without this, minting a new link
            after copying the old one would still read "Copied" for a link
            nobody has actually copied yet. */}
        <InviteLinkShare key={effectiveLink} link={effectiveLink} />
        <p className="text-[11.5px] text-muted">Shown once. You can get a new link any time.</p>
        <div className="flex flex-wrap items-center gap-2.5">
          <NewLinkControl
            id={invite.id}
            triggerLabel="Get a new link"
            confirmBody="The link above will stop working the moment you get a new one."
            onNewLink={handleNewLink}
          />
          {withdrawControl}
        </div>
      </li>
    );
  }

  // State 3 -- waiting, link not in hand (this session never minted one, or
  // the tab that did has since closed or reloaded).
  return (
    <li className={CARD_CLASS}>
      <div>
        <div className="text-[13.5px] font-semibold text-ink">{invite.name}</div>
        <div className="text-[11.5px] text-muted">{pendingInviteExpiryLine(invite.expiresAt)}</div>
      </div>
      <div className="flex flex-wrap items-center gap-2.5">
        <NewLinkControl
          id={invite.id}
          triggerLabel="Get a new link"
          confirmBody="This mints a new link and stops any earlier one from working."
          onNewLink={handleNewLink}
        />
        {withdrawControl}
      </div>
    </li>
  );
}
