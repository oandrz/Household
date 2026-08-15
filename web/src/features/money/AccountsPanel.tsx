// The accounts list: every row this caller is allowed to see, in the order
// the server returned them. Renders identically for an owner and a limited
// member except for one thing -- whether `account.balance` exists on the
// object at all (accountSchema.optional(), matching accountDTO's omitempty)
// -- so there is no separate "redacted row" branch here to keep in sync with
// the server's own redaction; there is only "show the balance if it's there."
//
// Edit, archive and restore are owner-only affordances added here, gated the
// same way the "+ Add account" button already was -- this is presentation,
// not the enforcement; every route behind these buttons still sits behind
// requireOwner on the server regardless.
import { useState } from "react";
import { useMe, useCurrencies } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { AccountModal } from "./AccountModal";
import { ACCOUNT_TYPE_LABELS } from "./accountTypes";
import { FINANCES_COPY } from "./copy";
import { formatMoney } from "./formatMoney";
import { useSetAccountArchived } from "./useAccounts";
import type { Account } from "./schemas";

function AccountRow({
  account,
  symbolFor,
  isOwner,
  pending,
  onEdit,
  onArchive,
  onRestore,
}: {
  account: Account;
  symbolFor: (currency: string) => string | undefined;
  isOwner: boolean;
  // True while an archive/restore call for *this* account is in flight --
  // scoped per row (not one panel-wide flag) for the same reason
  // MembersPanel's pendingIds is: a single useSetAccountArchived instance is
  // shared by every row, so its own isPending reflects only the most
  // recently dispatched call, not "is a call for this row still outstanding."
  pending: boolean;
  onEdit: (account: Account) => void;
  onArchive: (id: string) => void;
  onRestore: (id: string) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-hairline py-3 last:border-b-0">
      <div>
        <div className="text-[13.5px] font-semibold text-ink">
          {account.nickname}
          {account.archivedAt && (
            <span className="ml-1.5 text-[11px] font-normal text-muted">(archived)</span>
          )}
        </div>
        <div className="text-[11.5px] text-muted">
          {/* Two leaf spans, not one interpolated string: a screen with a
              single cash account also shows "Cash & savings" as a breakdown
              bar label, and a test (or a screen reader) asking for that exact
              text needs a node whose own content is only that -- not
              "Shared · Cash & savings" as one run. */}
          <span>{account.ownerName ?? "Shared"}</span>
          <span aria-hidden="true"> · </span>
          <span>{ACCOUNT_TYPE_LABELS[account.type]}</span>
        </div>
      </div>
      <div className="flex items-center gap-3">
        {account.balance && (
          <div className="text-[14px] font-semibold text-ink">
            {formatMoney(
              account.balance.amountMinor,
              account.balance.currency,
              symbolFor(account.balance.currency),
            )}
          </div>
        )}
        {isOwner && (
          <div className="flex items-center gap-2 text-[11px] font-semibold text-accent">
            {/* min-h-11/sm:min-h-0 on every button below: TransactionFilters.tsx's
                own SELECT_CLASS comment has the measured reason a control
                this small falls short of the 44px floor on a phone. */}
            <button
              type="button"
              aria-label={`Edit ${account.nickname}`}
              onClick={() => onEdit(account)}
              className="min-h-11 sm:min-h-0"
            >
              Edit
            </button>
            {account.archivedAt ? (
              <button
                type="button"
                aria-label={`Restore ${account.nickname}`}
                disabled={pending}
                onClick={() => onRestore(account.id)}
                className="min-h-11 disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                Restore
              </button>
            ) : (
              // Not window.confirm: a browser modal blocks the extension used
              // for the browser walk, and archiving is reversible from the
              // archived view anyway.
              <button
                type="button"
                aria-label={`Archive ${account.nickname}`}
                disabled={pending}
                onClick={() => onArchive(account.id)}
                className="min-h-11 text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                Archive
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export function AccountsPanel({
  accounts,
  includeArchived,
  onIncludeArchivedChange,
  emptyMessage,
}: {
  accounts: Account[];
  includeArchived: boolean;
  onIncludeArchivedChange: (next: boolean) => void;
  // Only the limited-member call site passes this: an owner reaching an empty
  // AccountsPanel has already been routed to FirstRunPanel by FinancesPage, so
  // there is no owner-facing copy for "list rendered, nothing in it."
  emptyMessage?: string;
}) {
  const me = useMe();
  const currencies = useCurrencies();
  const isOwner = me.data?.membership.role === "owner";
  const setArchived = useSetAccountArchived();

  // Not window.confirm and not this.mutation.isPending: a single
  // useSetAccountArchived instance is shared by every row (one mutation, not
  // one per account), so its own isPending only ever reflects the most
  // recently dispatched call. pendingIds tracks which account ids currently
  // have a call in flight, the same shape MembersPanel's own pendingIds uses
  // for the identical shared-mutation problem.
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());
  const [addOpen, setAddOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);

  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  const noneArchived = includeArchived && accounts.every((a) => a.archivedAt === null);

  function handleSetArchived(id: string, archived: boolean) {
    setPendingIds((prev) => new Set(prev).add(id));
    setArchived.mutate(
      { id, archived },
      {
        onSettled: () => {
          setPendingIds((prev) => {
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
        },
      },
    );
  }

  return (
    <section
      aria-labelledby="accounts-heading"
      className="flex flex-col gap-2 rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-center justify-between gap-3">
        <h2 id="accounts-heading" className="text-sm font-semibold text-ink">
          {FINANCES_COPY.accounts}
        </h2>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-1.5 text-[11px] text-muted">
            <ToggleSwitch
              checked={includeArchived}
              onChange={() => onIncludeArchivedChange(!includeArchived)}
              label={FINANCES_COPY.archivedToggle}
            />
            {FINANCES_COPY.archivedToggle}
          </div>
          {isOwner && (
            <button
              type="button"
              onClick={() => setAddOpen(true)}
              className="min-h-11 text-xs font-semibold text-accent sm:min-h-0"
            >
              {FINANCES_COPY.addAccount}
            </button>
          )}
        </div>
      </div>

      {noneArchived && <p className="text-xs text-muted">{FINANCES_COPY.archivedEmpty}</p>}

      {accounts.length === 0 && emptyMessage && (
        <p className="text-xs text-muted">{emptyMessage}</p>
      )}

      {accounts.length > 0 && (
        <div className="flex flex-col">
          {accounts.map((account) => (
            <AccountRow
              key={account.id}
              account={account}
              symbolFor={symbolFor}
              isOwner={isOwner}
              pending={pendingIds.has(account.id)}
              onEdit={setEditingAccount}
              onArchive={(id) => handleSetArchived(id, true)}
              onRestore={(id) => handleSetArchived(id, false)}
            />
          ))}
        </div>
      )}

      {/* Mounted only while actually open, unlike InviteMemberModal/
          NewSpaceModal's always-mounted shape: this modal's own queries
          (useMe, the household members list) would otherwise fire on every
          Finances page load whether or not anyone opens it -- wastefully in
          production, and unstubbed in every existing test of this panel that
          doesn't expect them. */}
      {addOpen && <AccountModal open onClose={() => setAddOpen(false)} />}
      {editingAccount && (
        <AccountModal open account={editingAccount} onClose={() => setEditingAccount(null)} />
      )}
    </section>
  );
}
