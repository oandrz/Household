// Settings' Access panel: every live way into the household that is not a
// password -- API tokens and linked Telegram chats -- each labelled with its
// member. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
//
// An owner gets every member's rows and a limited member only their own;
// the server decides (GET /household/access), so nothing here branches on
// role for *what to list*. The one role check is for the pending-invite
// pointer, whose list is owner-only: a limited member must never send a
// request whose 403 would then need hiding (usePendingInvites.ts).
import { useState } from "react";
import { useMe } from "../auth/useAuth";
import { ApiTokenList } from "./ApiTokenList";
import { pendingInvitesPointer } from "./copy";
import { LinkedChatList } from "./LinkedChatList";
import { NewApiTokenModal } from "./NewApiTokenModal";
import { useHouseholdAccess } from "./useHouseholdAccess";
import { usePendingInvites } from "./usePendingInvites";

export function AccessPanel() {
  const me = useMe();
  const access = useHouseholdAccess();
  const isOwner = me.data?.membership.role === "owner";
  const invites = usePendingInvites({ enabled: isOwner });
  const [creating, setCreating] = useState(false);

  const pointer = isOwner ? pendingInvitesPointer(invites.data?.length ?? 0) : null;

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="mb-1 flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink">Access</h2>
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
        >
          New token
        </button>
      </div>
      <p className="mb-4 text-xs text-muted">
        {isOwner ? "Every way into your household besides a password." : "Your own ways in besides a password."}
      </p>

      {pointer && (
        <p className="mb-4 text-xs">
          {/* The Members card is on this same page, so this scrolls rather
              than navigates. */}
          <button
            type="button"
            onClick={() => document.getElementById("members")?.scrollIntoView({ behavior: "smooth" })}
            // min-h-11/sm:min-h-0: same padding-less-button gap
            // MembersPanel's "+ Invite" button comments on -- no padding to
            // reach the 44px floor without this.
            className="min-h-11 font-semibold text-accent sm:min-h-0"
          >
            {pointer}
          </button>
        </p>
      )}

      {(access.isPending || me.isPending) && <p className="text-xs text-muted">Loading…</p>}
      {access.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load the access list.
        </p>
      )}

      {access.isSuccess && me.isSuccess && (
        <div className="flex flex-col gap-5">
          <div>
            <h3 className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">API tokens</h3>
            <ApiTokenList tokens={access.data.tokens} myUserId={me.data.user.id} />
          </div>
          {access.data.telegramEnabled && (
            <div>
              <h3 className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Linked chats</h3>
              <LinkedChatList chats={access.data.chats} myUserId={me.data.user.id} />
            </div>
          )}
        </div>
      )}

      <NewApiTokenModal open={creating} onClose={() => setCreating(false)} />
    </section>
  );
}
