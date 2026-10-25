// Money > Cashflow: the Monthly / Yearly tabs, the year picker, and the
// loading, error and empty states. Composition only: the numbers come from
// the server and the wording from cashflowCopy.ts.
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { cashflowCopy } from "./cashflowCopy";
import { CashflowMonthly } from "./CashflowMonthly";
import { CashflowYearly } from "./CashflowYearly";
import { formatMoney } from "./formatMoney";
import { useCashflow } from "./useCashflow";

type Tab = "monthly" | "yearly";

const TAB_BASE = "min-h-11 rounded-lg px-3.5 text-[13px] font-semibold lg:min-h-0 lg:py-1.5";

const TABS = ["monthly", "yearly"] as const;

type Props = {
  initialView?: Tab;
  initialYear?: number;
};

export function CashflowPage({ initialView = "monthly", initialYear }: Props) {
  const [tab, setTab] = useState<Tab>(initialView);
  // undefined means "this year", which the server works out in the
  // household's own time zone.
  const [year, setYear] = useState<number | undefined>(initialYear);
  const [infoOpen, setInfoOpen] = useState(false);
  const tabRefs = useRef<Record<Tab, HTMLButtonElement | null>>({ monthly: null, yearly: null });
  const navigate = useNavigate();

  // The address can change without this page remounting (the sidebar link
  // back to a plain /money/cashflow while on Yearly), so the screen follows
  // it. Our own writeAddress calls land on values the state already holds.
  useEffect(() => {
    setTab(initialView);
    setYear(initialYear);
  }, [initialView, initialYear]);

  const cashflow = useCashflow(year);
  const currencies = useCurrencies();

  // The address is rewritten in place, not pushed: stepping through years
  // should not fill the Back button. Monthly and the current year carry no
  // parameter, so a plain /money/cashflow is always "this year, Monthly".
  function writeAddress(nextTab: Tab, nextYear: number | undefined) {
    void navigate({
      to: ".",
      replace: true,
      search: {
        ...(nextTab === "yearly" ? { view: "yearly" as const } : {}),
        ...(nextYear === undefined ? {} : { year: nextYear }),
      },
    });
  }

  // A year typed into the address that the server refuses (a future year, or
  // one it has no data for) falls back to this year instead of an error: the
  // address is untrusted input, not something the person did wrong on screen.
  const yearRefused =
    year !== undefined &&
    cashflow.error instanceof ApiError &&
    cashflow.error.code === "CASHFLOW_YEAR_OUT_OF_RANGE";
  useEffect(() => {
    if (!yearRefused) return;
    setYear(undefined);
    writeAddress(tab, undefined);
    // writeAddress only reads navigate and its arguments.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [yearRefused]);

  function chooseTab(next: Tab) {
    setTab(next);
    writeAddress(next, year);
  }

  function onTabKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const next = TABS[(TABS.indexOf(tab) + (event.key === "ArrowRight" ? 1 : TABS.length - 1)) % TABS.length];
    chooseTab(next);
    tabRefs.current[next]?.focus();
  }

  if (cashflow.isLoading || yearRefused) {
    return <p className="p-9 text-xs text-muted">{cashflowCopy.loading}</p>;
  }
  if (cashflow.error) {
    // The read is owner-only on the server, so a member who holds the money
    // capability reaches this route and is refused there. Branch on what the
    // server decided rather than a second role check that could disagree.
    const status = cashflow.error instanceof ApiError ? cashflow.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="cashflow-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{cashflowCopy.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{cashflowCopy.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{cashflowCopy.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="cashflow-load-error" className="p-9 text-xs text-danger">
        {cashflowCopy.loadError}
      </p>
    );
  }
  if (!cashflow.data) return null;

  const data = cashflow.data;
  const symbol = currencies.data?.currencies.find((c) => c.code === data.currency)?.symbol;
  const formatAmount = (minor: number) => formatMoney(minor, data.currency, symbol);
  const currentYear = Number(data.current.month.slice(0, 4));

  return (
    <PageContainer data-testid="cashflow-page">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{cashflowCopy.title}</h1>
          <p className="mt-1 text-[13px] text-muted">{cashflowCopy.subtitle}</p>
        </div>
        <button
          type="button"
          aria-label={cashflowCopy.infoButton}
          aria-expanded={infoOpen}
          onClick={() => setInfoOpen((open) => !open)}
          className="min-h-11 min-w-11 shrink-0 rounded-full border border-hairline text-[13px] font-semibold text-ink transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off lg:min-h-0 lg:min-w-0 lg:px-3 lg:py-1"
        >
          i
        </button>
      </div>

      {infoOpen && (
        <section data-testid="cashflow-info" className="rounded-xl border border-hairline bg-card p-[22px]">
          <h2 className="text-[13px] font-semibold text-ink">{cashflowCopy.infoTitle}</h2>
          <ul className="mt-2 flex list-disc flex-col gap-1.5 pl-5 text-[13px] leading-relaxed text-ink">
            {cashflowCopy.infoPanel.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </section>
      )}

      {!data.hasTransactions ? (
        <div data-testid="cashflow-empty" className="rounded-xl border border-hairline bg-card p-16 text-center">
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">{cashflowCopy.emptyHeadline}</div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">{cashflowCopy.emptyBody}</p>
          <div className="mt-6 flex justify-center">
            <Link
              to="/money/transactions"
              className="inline-flex min-h-11 items-center rounded-lg bg-accent px-5 py-2.5 text-[13px] font-semibold text-white lg:min-h-0"
            >
              {cashflowCopy.addTransaction}
            </Link>
          </div>
        </div>
      ) : (
        <>
          <div role="tablist" className="flex gap-1.5">
            {TABS.map((name) => (
              <button
                key={name}
                ref={(el) => {
                  tabRefs.current[name] = el;
                }}
                id={`cashflow-tab-${name}`}
                type="button"
                role="tab"
                aria-selected={tab === name}
                aria-controls="cashflow-panel"
                tabIndex={tab === name ? 0 : -1}
                onClick={() => chooseTab(name)}
                onKeyDown={onTabKeyDown}
                className={`${TAB_BASE} ${
                  tab === name
                    ? "bg-accent text-white"
                    : "border border-hairline text-ink transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off"
                }`}
              >
                {name === "monthly" ? cashflowCopy.monthlyTab : cashflowCopy.yearlyTab}
              </button>
            ))}
          </div>
          <div role="tabpanel" id="cashflow-panel" aria-labelledby={`cashflow-tab-${tab}`}>
            {tab === "monthly" ? (
              <CashflowMonthly data={data} formatAmount={formatAmount} />
            ) : (
              <CashflowYearly
                data={data}
                formatAmount={formatAmount}
                onSelectYear={(picked) => {
                  const next = picked === currentYear ? undefined : picked;
                  setYear(next);
                  writeAddress(tab, next);
                }}
              />
            )}
          </div>
        </>
      )}
    </PageContainer>
  );
}
