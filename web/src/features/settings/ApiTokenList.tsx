// The API tokens group of the Access panel. Every row the server sent is
// shown; Revoke only on the caller's own, because DELETE /auth/tokens/{id}
// only ever revokes the caller's own (spec decision 6) -- a button on
// someone else's row would be a button that always 404s.
//
// The confirm step is in-page (useConfirmAction), never window.confirm, for
// the reason PendingInviteCard.tsx gives.
import { useConfirmAction } from "../../components/useConfirmAction";
import { tokenMetaLine } from "./copy";
import type { AccessToken } from "./schemas";
import { useRevokeApiToken } from "./useHouseholdAccess";

export function ApiTokenList({ tokens, myUserId }: { tokens: AccessToken[]; myUserId: string }) {
  if (tokens.length === 0) {
    return <p className="text-xs text-muted">No API tokens.</p>;
  }
  return (
    <ul className="flex flex-col divide-y divide-hairline">
      {tokens.map((t) => (
        <TokenRow key={t.id} token={t} isMine={t.memberId === myUserId} />
      ))}
    </ul>
  );
}

function TokenRow({ token, isMine }: { token: AccessToken; isMine: boolean }) {
  const revoke = useRevokeApiToken();
  const action = useConfirmAction("Couldn't revoke that token. Please try again.");
  const error = action.errorFor();

  return (
    <li className="flex flex-col gap-1.5 py-2.5 text-[13px]">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-ink">
            <span className="font-semibold">{token.name}</span>{" "}
            <span className="text-muted">· {token.memberName}</span>
          </div>
          <div className="mt-0.5 text-[11.5px] text-muted">
            <span className="font-mono">{token.prefix}…</span> · {tokenMetaLine(token)}
          </div>
        </div>
        {isMine && !action.isConfirming() && (
          <button
            type="button"
            onClick={() => action.ask()}
            disabled={action.isPending()}
            className="min-h-11 shrink-0 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            Revoke
          </button>
        )}
        {isMine && action.isConfirming() && (
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={() => void action.confirm(() => revoke.mutateAsync(token.id))}
              disabled={action.isPending()}
              className="min-h-11 rounded-lg bg-danger px-3 py-1.5 text-[11px] font-semibold text-white disabled:opacity-60 sm:min-h-0"
            >
              Yes, revoke
            </button>
            <button
              type="button"
              onClick={() => action.cancel()}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
            >
              Keep
            </button>
          </div>
        )}
      </div>
      {error && (
        <p role="alert" className="text-[11px] text-danger">
          {error}
        </p>
      )}
    </li>
  );
}
