// The accounts list: every row this caller is allowed to see, in the order
// the server returned them. Renders identically for an owner and a limited
// member except for one thing -- whether `account.balance` exists on the
// object at all (accountSchema.optional(), matching accountDTO's omitempty)
// -- so there is no separate "redacted row" branch here to keep in sync with
// the server's own redaction; there is only "show the balance if it's there."
import { useMe, useCurrencies } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { ACCOUNT_TYPE_LABELS } from "./accountTypes";
import { FINANCES_COPY } from "./copy";
import { formatMoney } from "./formatMoney";
import type { Account } from "./schemas";

function AccountRow({
  account,
  symbolFor,
}: {
  account: Account;
  symbolFor: (currency: string) => string | undefined;
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
      {account.balance && (
        <div className="text-[14px] font-semibold text-ink">
          {formatMoney(
            account.balance.amountMinor,
            account.balance.currency,
            symbolFor(account.balance.currency),
          )}
        </div>
      )}
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

  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  const noneArchived = includeArchived && accounts.every((a) => a.archivedAt === null);

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
            // Not yet wired to anything -- the create form is Task 40's. The
            // button exists now, gated the same way it always will be, so
            // that task only has to add an onClick rather than also decide
            // who gets to see it.
            <button type="button" className="text-xs font-semibold text-accent">
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
            <AccountRow key={account.id} account={account} symbolFor={symbolFor} />
          ))}
        </div>
      )}
    </section>
  );
}
