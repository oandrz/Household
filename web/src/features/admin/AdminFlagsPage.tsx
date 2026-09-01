// The admin surface's first (and so far only) page: every feature flag this
// build's registry defines, plus any override row naming a flag the
// registry has since dropped.
//
// A flag's global state is not a boolean -- domain.FlagKey's own registry
// tracks "no opinion" (globalSet: false, the compile-time default applies),
// "explicitly on" and "explicitly off" as three distinct states, and
// deleting a household's override is a different action from setting it
// false (handleClearHouseholdFlag's own comment: "'No opinion' and
// 'explicitly off' are different states"). Every control below keeps that
// distinction visible rather than collapsing it into a single switch.
import { useState } from "react";
import { PageContainer } from "../../components/PageContainer";
import {
  isAdminLayerFailure,
  useAdminFlags,
  useClearHouseholdFlag,
  useSetGlobalFlag,
  useSetHouseholdFlag,
  type AdminFlag,
} from "./useAdmin";

// There is no endpoint to clear a *global* override back to "no opinion"
// (router.go registers PUT for the global value but no DELETE, unlike the
// household route below) -- SEGMENTS's "Default" slot is therefore a status
// indicator, never a button: it is only ever the current state, and only
// before the first PUT this flag has ever received.
const SEGMENTS = [
  { value: "default", label: "Default" },
  { value: "on", label: "On" },
  { value: "off", label: "Off" },
] as const;

function globalSegment(flag: AdminFlag): (typeof SEGMENTS)[number]["value"] {
  if (!flag.globalSet) return "default";
  return flag.globalEnabled ? "on" : "off";
}

function GlobalFlagControl({
  flag,
  onSet,
  pending,
}: {
  flag: AdminFlag;
  onSet: (enabled: boolean) => void;
  pending: boolean;
}) {
  const current = globalSegment(flag);
  return (
    <div role="group" aria-label={`Global value for ${flag.key}`} className="inline-flex rounded-lg border border-hairline p-0.5">
      {SEGMENTS.map((segment) => {
        const isCurrent = segment.value === current;
        if (segment.value === "default") {
          return (
            <span
              key={segment.value}
              aria-current={isCurrent ? "true" : undefined}
              className={`rounded-md px-2.5 py-1 text-[12px] font-semibold ${
                isCurrent ? "bg-canvas text-ink" : "text-muted"
              }`}
            >
              {segment.label}
            </span>
          );
        }
        return (
          <button
            key={segment.value}
            type="button"
            aria-pressed={isCurrent}
            disabled={pending}
            onClick={() => onSet(segment.value === "on")}
            className={`rounded-md px-2.5 py-1 text-[12px] font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
              isCurrent ? "bg-accent text-white" : "text-label hover:bg-canvas"
            }`}
          >
            {segment.label}
          </button>
        );
      })}
    </div>
  );
}

function OverrideRow({
  flag,
  householdName,
  enabled,
  onSet,
  onClear,
  pending,
}: {
  flag: AdminFlag;
  householdName: string;
  enabled: boolean;
  onSet: (enabled: boolean) => void;
  onClear: () => void;
  pending: boolean;
}) {
  return (
    <li className="flex items-center justify-between gap-3 py-1.5 text-[12.5px]">
      <span className="text-ink">{householdName}</span>
      <span className="flex items-center gap-2">
        <button
          type="button"
          aria-pressed={enabled}
          disabled={pending}
          onClick={() => onSet(!enabled)}
          className={`rounded-md px-2 py-0.5 text-[11.5px] font-semibold disabled:cursor-not-allowed disabled:opacity-60 ${
            enabled ? "bg-accent text-white" : "bg-toggle-off text-label"
          }`}
        >
          {enabled ? "On" : "Off"}
        </button>
        <button
          type="button"
          disabled={pending}
          onClick={onClear}
          aria-label={`Remove ${flag.key} override for ${householdName}`}
          className="text-[11.5px] font-medium text-danger disabled:cursor-not-allowed disabled:opacity-60"
        >
          Remove
        </button>
      </span>
    </li>
  );
}

function OverridesDisclosure({
  flag,
  onSetHousehold,
  onClearHousehold,
  pendingHouseholdId,
}: {
  flag: AdminFlag;
  onSetHousehold: (householdId: string, enabled: boolean) => void;
  onClearHousehold: (householdId: string) => void;
  pendingHouseholdId: string | null;
}) {
  const [open, setOpen] = useState(false);
  const count = flag.overrides.length;

  if (count === 0) {
    return <p className="text-[12px] text-muted">No household overrides.</p>;
  }

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="text-[12px] font-semibold text-accent"
      >
        {count} household override{count === 1 ? "" : "s"}
      </button>
      {open && (
        <ul className="mt-1.5 border-t border-hairline pt-1.5">
          {flag.overrides.map((override) => (
            <OverrideRow
              key={override.householdId}
              flag={flag}
              householdName={override.householdName}
              enabled={override.enabled}
              pending={pendingHouseholdId === override.householdId}
              onSet={(enabled) => onSetHousehold(override.householdId, enabled)}
              onClear={() => onClearHousehold(override.householdId)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function FlagRow({
  flag,
  onSetGlobal,
  onSetHousehold,
  onClearHousehold,
  pendingGlobal,
  pendingHouseholdId,
}: {
  flag: AdminFlag;
  onSetGlobal: (enabled: boolean) => void;
  onSetHousehold: (householdId: string, enabled: boolean) => void;
  onClearHousehold: (householdId: string) => void;
  pendingGlobal: boolean;
  pendingHouseholdId: string | null;
}) {
  return (
    <li
      data-testid={`flag-row-${flag.key}`}
      className="flex flex-col gap-2 border-b border-hairline py-4 last:border-b-0 sm:flex-row sm:items-start sm:justify-between sm:gap-6"
    >
      <div className="min-w-0">
        <code className="text-[13px] font-semibold text-ink">{flag.key}</code>
        <p className="mt-0.5 text-[12.5px] text-muted">{flag.description}</p>
        <p className="mt-1 text-[11.5px] text-label">
          Compile-time default: {flag.default ? "On" : "Off"}
        </p>
        <div className="mt-2">
          <OverridesDisclosure
            flag={flag}
            onSetHousehold={onSetHousehold}
            onClearHousehold={onClearHousehold}
            pendingHouseholdId={pendingHouseholdId}
          />
        </div>
      </div>
      <GlobalFlagControl flag={flag} onSet={onSetGlobal} pending={pendingGlobal} />
    </li>
  );
}

function OrphanedFlagRow({ flag, onClearHousehold, pendingHouseholdId }: {
  flag: AdminFlag;
  onClearHousehold: (householdId: string) => void;
  pendingHouseholdId: string | null;
}) {
  return (
    <li
      data-testid={`orphaned-flag-row-${flag.key}`}
      className="flex flex-col gap-1.5 border-b border-hairline py-3 last:border-b-0"
    >
      <code className="text-[13px] font-semibold text-ink">{flag.key}</code>
      <ul>
        {flag.overrides.map((override) => (
          <li key={override.householdId} className="flex items-center justify-between gap-3 py-1 text-[12.5px]">
            <span className="text-ink">{override.householdName}</span>
            <button
              type="button"
              disabled={pendingHouseholdId === override.householdId}
              onClick={() => onClearHousehold(override.householdId)}
              aria-label={`Remove ${flag.key} override for ${override.householdName}`}
              className="text-[11.5px] font-medium text-danger disabled:cursor-not-allowed disabled:opacity-60"
            >
              Remove
            </button>
          </li>
        ))}
      </ul>
    </li>
  );
}

// A plain string, so the banner below never has to reason about ApiError vs.
// a generic Error vs. whatever a rejected promise happened to throw -- and
// so its type is unambiguously renderable, unlike `unknown`.
function writeErrorMessageFor(error: Error): string {
  return error.message || "That change didn't go through.";
}

export function AdminFlagsPage() {
  const flagsQuery = useAdminFlags();
  const setGlobal = useSetGlobalFlag();
  const setHousehold = useSetHouseholdFlag();
  const clearHousehold = useClearHouseholdFlag();
  // The key currently mid-mutation, so only the one control that triggered a
  // write disables itself -- a page-wide pending flag would freeze every
  // other flag's controls while one PUT is in flight, for no reason a
  // caller could see.
  const [pendingGlobalKey, setPendingGlobalKey] = useState<string | null>(null);
  const [pendingHouseholdId, setPendingHouseholdId] = useState<string | null>(null);

  // A write's own failure that isn't an admin-layer signal (an unknown flag
  // key, a lookup error) has nowhere else to go, so it's shown here, next to
  // the list it happened on. ADMIN_REAUTH_REQUIRED and NOT_FOUND are
  // filtered out with the identical isAdminLayerFailure check
  // closeSurfaceOnAdminLayerFailure (useAdmin.ts) uses to decide whether to
  // invalidate adminFlagsKey -- one shared predicate, so this banner can
  // never show a message for a failure AdminShell's own gate is about to
  // replace the whole surface for anyway.
  //
  // Kept as {error, reset} pairs, not just the error: the "Dismiss" button
  // below has to clear the one mutation that actually produced the message
  // on screen, or a failed PUT's banner would otherwise outlive any number
  // of later successful writes on the other two hooks with no way to close
  // it.
  const writeFailure =
    [
      { error: setGlobal.error, reset: setGlobal.reset },
      { error: setHousehold.error, reset: setHousehold.reset },
      { error: clearHousehold.error, reset: clearHousehold.reset },
    ].find((candidate): candidate is { error: Error; reset: () => void } =>
      candidate.error !== null && !isAdminLayerFailure(candidate.error),
    ) ?? null;

  function handleSetGlobal(key: string, enabled: boolean) {
    setPendingGlobalKey(key);
    setGlobal.mutate({ key, enabled }, { onSettled: () => setPendingGlobalKey(null) });
  }
  function handleSetHousehold(key: string, householdId: string, enabled: boolean) {
    setPendingHouseholdId(householdId);
    setHousehold.mutate({ key, householdId, enabled }, { onSettled: () => setPendingHouseholdId(null) });
  }
  function handleClearHousehold(key: string, householdId: string) {
    setPendingHouseholdId(householdId);
    clearHousehold.mutate({ key, householdId }, { onSettled: () => setPendingHouseholdId(null) });
  }

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">Feature flags</h1>
      {writeFailure && (
        <div
          role="alert"
          className="flex items-start justify-between gap-3 rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          <span>{writeErrorMessageFor(writeFailure.error)}</span>
          <button
            type="button"
            onClick={writeFailure.reset}
            aria-label="Dismiss"
            className="flex-none font-bold leading-none text-danger"
          >
            ×
          </button>
        </div>
      )}
      {flagsQuery.isPending ? (
        <p className="text-[13px] text-muted">Loading…</p>
      ) : flagsQuery.data ? (
        <FlagsList
          flags={flagsQuery.data.flags}
          pendingGlobalKey={pendingGlobalKey}
          pendingHouseholdId={pendingHouseholdId}
          onSetGlobal={handleSetGlobal}
          onSetHousehold={handleSetHousehold}
          onClearHousehold={handleClearHousehold}
        />
      ) : null}
    </PageContainer>
  );
}

function FlagsList({
  flags,
  pendingGlobalKey,
  pendingHouseholdId,
  onSetGlobal,
  onSetHousehold,
  onClearHousehold,
}: {
  flags: AdminFlag[];
  pendingGlobalKey: string | null;
  pendingHouseholdId: string | null;
  onSetGlobal: (key: string, enabled: boolean) => void;
  onSetHousehold: (key: string, householdId: string, enabled: boolean) => void;
  onClearHousehold: (key: string, householdId: string) => void;
}) {
  const active = flags.filter((flag) => !flag.orphaned);
  const orphaned = flags.filter((flag) => flag.orphaned);

  return (
    <>
      <ul>
        {active.map((flag) => (
          <FlagRow
            key={flag.key}
            flag={flag}
            pendingGlobal={pendingGlobalKey === flag.key}
            pendingHouseholdId={pendingHouseholdId}
            onSetGlobal={(enabled) => onSetGlobal(flag.key, enabled)}
            onSetHousehold={(householdId, enabled) => onSetHousehold(flag.key, householdId, enabled)}
            onClearHousehold={(householdId) => onClearHousehold(flag.key, householdId)}
          />
        ))}
      </ul>

      {orphaned.length > 0 && (
        <section className="mt-6 rounded-xl border border-hairline bg-canvas p-4">
          <h2 className="text-[13px] font-semibold text-ink">Orphaned — safe to delete</h2>
          <p className="mt-0.5 text-[12px] text-muted">
            These override a flag this build no longer defines. They enable nothing.
          </p>
          <ul className="mt-2">
            {orphaned.map((flag) => (
              <OrphanedFlagRow
                key={flag.key}
                flag={flag}
                pendingHouseholdId={pendingHouseholdId}
                onClearHousehold={(householdId) => onClearHousehold(flag.key, householdId)}
              />
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
