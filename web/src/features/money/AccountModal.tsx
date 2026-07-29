// The add/edit account form (design/Household Dashboard.dc.html's "3c" panel,
// step 3 of its bank-connect wizard). Both the connected-bank header strip
// ("DBS Multiplier ····6021 · Connected") and the "Choose / Connect / Details"
// step indicator above it describe a sync this product doesn't have -- there
// is no step 1 or 2 here, only this form, so showing a stepper claiming two
// already-completed steps would be its own small lie. What's kept is the
// title, the field list and the two toggles' literal copy.
//
// Owner and Type are rendered as native <select>s here, not the design's pill
// buttons -- the brief that specified this form calls for a select
// explicitly, and a native select is what lets a test (and a keyboard user)
// change either field with one event instead of simulating a row of buttons.
import { type FormEvent, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal } from "../../components/Modal";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { useCurrencies, useMe } from "../auth/useAuth";
import { membersListSchema, type MemberView } from "../settings/schemas";
import { ACCOUNT_TYPES, ACCOUNT_TYPE_LABELS, LIABILITY_TYPES } from "./accountTypes";
import { minorUnitsToInputValue, NO_DECIMAL_CURRENCIES, toMinorUnits } from "./formatMoney";
import { useCreateAccount, useUpdateAccount } from "./useAccounts";
import type { Account, AccountType } from "./schemas";

// AccountFormValues is exactly the POST body the create route accepts, so the
// modal and useCreateAccount cannot disagree about field names.
export type AccountFormValues = {
  nickname: string;
  type: AccountType;
  ownerMembershipId: string | null;
  openingBalanceMinor: number;
  openingBalanceCurrency: string;
  openingBalanceAsOf: string;
  countTowardNetWorth: boolean;
  visibleToLimitedMembers: boolean;
};

// The PATCH route's own body. openingBalanceMinor/openingBalanceCurrency are
// optional here -- and genuinely omitted from the request, not just
// undefined -- unless the person actually touched Balance or Currency. See
// handleSubmit: an edit that only changes the nickname must never resend a
// balance derived from what the field displays, because the display can
// truncate a value the create path itself could never produce (an IDR/VND
// account whose stored minor units aren't a multiple of 100 -- unreachable
// from this form, but not prevented anywhere else: a direct API call, a CSV
// import or a future bank-sync adapter all could). Resending it regardless
// would move the stored figure by up to 99 minor units as a side effect of
// an edit that never touched it.
export type AccountEditValues = Omit<
  AccountFormValues,
  "openingBalanceMinor" | "openingBalanceCurrency"
> &
  Partial<Pick<AccountFormValues, "openingBalanceMinor" | "openingBalanceCurrency">>;

// The household member list this modal's Owner select needs. Shares
// MembersPanel's own ["household", "members"] query key (not exported from
// there, so re-declared here) rather than a differently-keyed fetch of the
// same endpoint, so an owner who has Settings' Members panel open in another
// tab and this modal open in this one are reading one cache entry, not two
// that can drift.
const householdMembersQueryKey = ["household", "members"] as const;

async function fetchHouseholdMembers(): Promise<MemberView[]> {
  const body = await apiFetch<unknown>("/api/v1/household/members");
  return membersListSchema.parse(body);
}

function useHouseholdMembers() {
  return useQuery({ queryKey: householdMembersQueryKey, queryFn: fetchHouseholdMembers });
}

// today() reads the *local* calendar date via getFullYear/getMonth/getDate,
// never toISOString() (which converts to UTC first). This project has hit
// exactly that mistake twice already on the backend (dateOnly's
// UTC().Truncate() -- see docs' "third instance of one mistake" commit): a
// caller at 7am in Singapore (UTC+8) computing "today" through UTC would get
// yesterday's date. There is no server round trip to catch that here, so this
// has to get it right on its own.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function AccountModal({
  open,
  onClose,
  account,
}: {
  open: boolean;
  onClose: () => void;
  // Present only when editing: every field below is seeded from it, and the
  // submitted request is a PATCH to this account's id rather than a POST.
  account?: Account;
}) {
  const isEditing = account !== undefined;
  const me = useMe();
  const currencies = useCurrencies();
  const members = useHouseholdMembers();
  const createAccount = useCreateAccount();
  const updateAccount = useUpdateAccount();

  const [nickname, setNickname] = useState(account?.nickname ?? "");
  const [type, setType] = useState<AccountType>(account?.type ?? "cash");
  const [ownerMembershipId, setOwnerMembershipId] = useState<string | null>(
    account?.ownerMembershipId ?? null,
  );
  // account.balance is the account's *current* balance (opening balance plus
  // every transaction since -- usecase/ports.go's own doc comment on
  // AccountView.Balance), and this prefill sends whatever the person leaves
  // here straight back as openingBalanceMinor. The two are the same number
  // today only because there is no transactions table yet: once Transactions
  // ships, prefilling from the current balance and writing it back as the
  // *opening* balance would rewrite history on every edit that touches this
  // field. The real-patch handling in handleSubmit below (only send the
  // balance when it was actually touched) protects the untouched case; a
  // touched edit will need this prefill to read from opening balance instead.
  const [balanceInput, setBalanceInput] = useState(() =>
    account?.balance ? minorUnitsToInputValue(account.balance.amountMinor, account.balance.currency) : "",
  );
  const [balanceTouched, setBalanceTouched] = useState(false);
  const [currency, setCurrency] = useState(() => account?.balance?.currency ?? "");
  const [currencyTouched, setCurrencyTouched] = useState(false);
  const [asOf, setAsOf] = useState(account?.balanceAsOf ?? today());
  const [countTowardNetWorth, setCountTowardNetWorth] = useState(account?.countTowardNetWorth ?? true);
  const [visibleToLimitedMembers, setVisibleToLimitedMembers] = useState(
    account?.visibleToLimitedMembers ?? false,
  );
  const [balanceError, setBalanceError] = useState<string | null>(null);

  // Only a fresh "add" form defaults to the household's primary currency --
  // editing an existing account must never override what it actually holds.
  // `me` can still be loading when this modal first mounts (this component's
  // own tests mount it with a cold QueryClient), so this is computed during
  // render and re-checked on every render rather than read once in a useState
  // initialiser, the same "derive from fetched data until the person takes
  // over" pattern CurrencyPanel's own primary-currency sync uses instead of a
  // separate effect.
  if (!isEditing && !currencyTouched && me.isSuccess && currency !== me.data.household.primaryCurrency) {
    setCurrency(me.data.household.primaryCurrency);
  }

  const mutation = isEditing ? updateAccount : createAccount;

  // Parses and validates the Balance field, setting the field error and
  // returning null on either failure. Shared by both submit paths below so
  // "not a number" and "that number has the wrong sign" can't drift between
  // create and edit.
  function parseValidatedBalance(): number | null {
    const minorUnits = toMinorUnits(balanceInput, currency);
    if (minorUnits === null) {
      // toMinorUnits also returns null when Balance still holds a figure with
      // cents and Currency was just switched to a no-decimal one (IDR, VND)
      // without touching Balance -- an SGD account showing "8240.55" stays
      // exactly that after switching to IDR, so restating it back as "enter
      // an amount, like 8240.55" describes the very thing already in the
      // field rather than what actually went wrong. hasADecimalPoint is
      // deliberately its own small check, not a reason toMinorUnits itself
      // returned null: it only ever changes which of two messages is shown,
      // never whether the value is accepted, so it staying in sync with
      // toMinorUnits's own number format is a copy concern, not a
      // correctness one.
      const hasADecimalPoint = /^-?\d+\.\d+$/.test(balanceInput.trim().replace(/,/g, ""));
      if (NO_DECIMAL_CURRENCIES.has(currency) && hasADecimalPoint) {
        setBalanceError(`${currency} doesn't use cents. Remove the decimal point.`);
        return null;
      }
      setBalanceError("Enter an amount, like 8240.55.");
      return null;
    }
    // The same rule domain.AccountType.SignedNetWorthAmount enforces, said
    // where someone can act on it. The backend refuses a negative debt
    // regardless (422 INVALID_BALANCE); this exists so the person finds out
    // while they are still looking at the field rather than after submitting.
    const debtIsNegative = LIABILITY_TYPES.has(type) && minorUnits < 0;
    if (debtIsNegative) {
      setBalanceError(
        "Enter what you owe as a positive amount. Hearth adds the minus sign for a loan or credit card.",
      );
      return null;
    }
    return minorUnits;
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const commonFields = {
      nickname,
      type,
      ownerMembershipId,
      openingBalanceAsOf: asOf,
      countTowardNetWorth,
      visibleToLimitedMembers,
    };

    if (isEditing) {
      // Real-patch handling: openingBalanceMinor/openingBalanceCurrency are
      // included only when the person actually touched Balance or Currency.
      // usecase.AccountUpdate treats an absent field as "leave this alone"
      // (TestUpdateIsARealPatch pins that on the Go side), so an edit that
      // never goes near the balance can never resend a value derived from
      // whatever the field happens to display -- see balanceInput's own
      // comment for why that display can disagree with the stored figure.
      const balanceWasTouched = balanceTouched || currencyTouched;
      let patch: AccountEditValues = commonFields;
      if (balanceWasTouched) {
        const minorUnits = parseValidatedBalance();
        if (minorUnits === null) return;
        patch = { ...commonFields, openingBalanceMinor: minorUnits, openingBalanceCurrency: currency };
      }
      setBalanceError(null);
      updateAccount.mutate({ id: account.id, ...patch }, { onSuccess: onClose });
      return;
    }

    const minorUnits = parseValidatedBalance();
    if (minorUnits === null) return;
    setBalanceError(null);

    const body: AccountFormValues = {
      ...commonFields,
      openingBalanceMinor: minorUnits,
      openingBalanceCurrency: currency,
    };
    createAccount.mutate(body, { onSuccess: onClose });
  }

  return (
    <Modal open={open} onClose={onClose} title="Account details">
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="account-nickname" className="text-xs font-semibold text-label">
            Nickname
          </label>
          <input
            id="account-nickname"
            type="text"
            required
            value={nickname}
            onChange={(event) => setNickname(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="account-owner" className="text-xs font-semibold text-label">
              Owner
            </label>
            <select
              id="account-owner"
              value={ownerMembershipId ?? ""}
              onChange={(event) =>
                setOwnerMembershipId(event.target.value === "" ? null : event.target.value)
              }
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              <option value="">Shared</option>
              {members.data?.map((member) => (
                <option key={member.id} value={member.id}>
                  {member.user.displayName}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="account-type" className="text-xs font-semibold text-label">
              Type
            </label>
            <select
              id="account-type"
              value={type}
              onChange={(event) => setType(event.target.value as AccountType)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              {ACCOUNT_TYPES.map((t) => (
                <option key={t} value={t}>
                  {ACCOUNT_TYPE_LABELS[t]}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="account-balance" className="text-xs font-semibold text-label">
              Balance
            </label>
            <input
              id="account-balance"
              type="text"
              inputMode="decimal"
              required
              value={balanceInput}
              onChange={(event) => {
                setBalanceTouched(true);
                setBalanceInput(event.target.value);
              }}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="account-currency" className="text-xs font-semibold text-label">
              Currency
            </label>
            <select
              id="account-currency"
              value={currency}
              onChange={(event) => {
                setCurrencyTouched(true);
                setCurrency(event.target.value);
              }}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              {currency === "" && <option value="">--</option>}
              {currencies.data?.currencies.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.code}
                </option>
              ))}
            </select>
          </div>
        </div>

        {balanceError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {balanceError}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor="account-as-of" className="text-xs font-semibold text-label">
            Balance as of
          </label>
          <input
            id="account-as-of"
            type="date"
            required
            value={asOf}
            onChange={(event) => setAsOf(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
          <div>
            <div className="text-[13px] text-ink">Count toward net worth</div>
            <div className="mt-0.5 text-[11.5px] text-muted">
              Include this balance in the family total
            </div>
          </div>
          <ToggleSwitch
            checked={countTowardNetWorth}
            onChange={() => setCountTowardNetWorth((v) => !v)}
            label="Count toward net worth"
          />
        </div>

        <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
          <div>
            <div className="text-[13px] text-ink">Visible to kids</div>
            {/* Literal design copy, true of this specific seeded household --
                the same non-generalised choice CurrencyPanel's "For
                Christine's Indonesian accounts" row already makes, flagged
                there for the same reason. */}
            <div className="mt-0.5 text-[11.5px] text-muted">
              Kayla &amp; Ethan can see this account exists, not the balance
            </div>
          </div>
          <ToggleSwitch
            checked={visibleToLimitedMembers}
            onChange={() => setVisibleToLimitedMembers((v) => !v)}
            label="Visible to kids"
          />
        </div>

        {mutation.isError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {apiErrorMessage(mutation.error, "Something went wrong. Please try again.")}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={mutation.isPending}
            className="flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isEditing ? "Save" : "Add account"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
