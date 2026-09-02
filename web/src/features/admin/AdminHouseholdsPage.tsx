// The operator's households list: four counters, an explicit search over
// households and members, and one row per household linking to its
// drill-in. URL state (q, limit) arrives as props and navigation goes back
// out through onSearch/onShowMore -- the route in router.tsx owns the URL,
// the same seam the magic-link route uses, so this component renders under
// renderWithRouter's bare root route in tests.
//
// Search is a form, never a keystroke listener: every request under /admin
// is an audit row (useAdminDirectory.ts), so one submitted search is one
// request and one row.
import { type FormEvent, useState } from "react";
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import type {
  AdminHouseholdListing,
  AdminHouseholdsResponse,
} from "./adminDirectorySchemas";
import {
  dateLabel,
  exactTimeLabel,
  noMatchLabel,
  relativeTimeLabel,
  showingLabel,
} from "./directoryCopy";
import {
  DIRECTORY_MAX_LIMIT,
  useAdminHouseholds,
  useCloseSurfaceOnReauth,
} from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

export function AdminHouseholdsPage({
  q,
  limit,
  onSearch,
  onShowMore,
}: {
  q: string;
  limit: number;
  onSearch: (q: string) => void;
  onShowMore: () => void;
}) {
  const query = useAdminHouseholds(q, limit);
  useCloseSurfaceOnReauth(query.error);
  const [draft, setDraft] = useState(q);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onSearch(draft.trim());
  }

  // A gate-layer failure (lapsed grant, admin revoked) is about to be
  // replaced by AdminShell's own gate; rendering it inline too would flash
  // a second message for the same failure.
  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
        Households
      </h1>

      <form
        role="search"
        onSubmit={handleSubmit}
        className="flex flex-col gap-1.5 sm:max-w-[480px]"
      >
        <label
          htmlFor="household-search"
          className="text-[12px] font-semibold text-label"
        >
          Search
        </label>
        <div className="flex items-center gap-2">
          <input
            id="household-search"
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="Household, member name or email"
            className="min-w-0 flex-1 rounded-lg border border-hairline bg-card px-3 py-1.5 text-[13px] text-ink"
          />
          <button
            type="submit"
            className="rounded-lg bg-accent px-3 py-1.5 text-[12.5px] font-semibold text-white active:translate-y-px"
          >
            Search
          </button>
          {q !== "" && (
            <button
              type="button"
              onClick={() => {
                setDraft("");
                onSearch("");
              }}
              className="text-[12.5px] font-medium text-muted hover:text-ink"
            >
              Clear
            </button>
          )}
        </div>
      </form>

      {inlineError && (
        <div
          role="alert"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          {inlineError instanceof ApiError
            ? inlineError.message
            : "Something went wrong loading the households."}
        </div>
      )}

      {query.isPending ? (
        <HouseholdsSkeleton />
      ) : query.data ? (
        <>
          <MetricTiles metrics={query.data.metrics} />
          <HouseholdsTable
            data={query.data}
            q={q}
            limit={limit}
            onClear={() => onSearch("")}
            onShowMore={onShowMore}
          />
        </>
      ) : null}
    </PageContainer>
  );
}

function HouseholdsSkeleton() {
  return (
    <div
      data-testid="households-skeleton"
      aria-hidden="true"
      className="flex flex-col gap-5"
    >
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div
            key={i}
            className="h-[72px] rounded-lg border border-hairline bg-canvas"
          />
        ))}
      </div>
      <div className="flex flex-col divide-y divide-hairline">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="h-10 bg-canvas" />
        ))}
      </div>
    </div>
  );
}

function MetricTiles({
  metrics,
}: {
  metrics: AdminHouseholdsResponse["metrics"];
}) {
  const tiles: { label: string; lines: string[] }[] = [
    { label: "Households", lines: [String(metrics.households)] },
    { label: "Active, 7 days", lines: [String(metrics.activeHouseholds7d)] },
    {
      label: "Sign-ups, 30 days",
      lines: [
        `${metrics.signups30d.requested} requested`,
        `${metrics.signups30d.completed} completed`,
      ],
    },
    { label: "Invites pending", lines: [String(metrics.pendingInvites)] },
  ];
  return (
    <ul
      aria-label="Install metrics"
      className="grid grid-cols-2 gap-3 lg:grid-cols-4"
    >
      {tiles.map((tile) => (
        <li
          key={tile.label}
          className="rounded-lg border border-hairline bg-card px-4 py-3"
        >
          <p className="text-[11.5px] font-semibold uppercase tracking-[0.04em] text-label">
            {tile.label}
          </p>
          {tile.lines.map((line) => (
            <p
              key={line}
              className="mt-1 text-[20px] font-medium tabular-nums text-ink"
            >
              {line}
            </p>
          ))}
        </li>
      ))}
    </ul>
  );
}

function HouseholdsTable({
  data,
  q,
  limit,
  onClear,
  onShowMore,
}: {
  data: AdminHouseholdsResponse;
  q: string;
  limit: number;
  onClear: () => void;
  onShowMore: () => void;
}) {
  const now = new Date();
  if (data.households.length === 0) {
    return q === "" ? (
      <p className="text-[13px] text-muted">No households yet.</p>
    ) : (
      <p className="text-[13px] text-muted">
        {noMatchLabel(q)}{" "}
        <button
          type="button"
          onClick={onClear}
          className="font-medium text-accent"
        >
          Clear
        </button>
      </p>
    );
  }
  const atCap = limit >= DIRECTORY_MAX_LIMIT;
  return (
    <div className="flex flex-col gap-3">
      <table className="w-full text-[12.5px]">
        <thead className="hidden md:table-header-group">
          <tr className="text-left text-[11.5px] font-semibold text-label">
            <th scope="col" className="py-1.5 pr-3">
              Name
            </th>
            <th scope="col" className="py-1.5 pr-3">
              Family
            </th>
            <th scope="col" className="py-1.5 pr-3">
              Members
            </th>
            <th scope="col" className="py-1.5 pr-3">
              Created
            </th>
            <th scope="col" className="py-1.5 pr-3">
              Last active
            </th>
            <th scope="col" className="py-1.5">
              Currency
            </th>
          </tr>
        </thead>
        <tbody>
          {data.households.map((h) => (
            <HouseholdRow key={h.id} household={h} now={now} />
          ))}
        </tbody>
      </table>
      <p className="flex items-center gap-3 text-[12px] text-muted">
        <span>
          {showingLabel(data.households.length, data.truncated, atCap)}
        </span>
        {data.truncated && !atCap && (
          <button
            type="button"
            onClick={onShowMore}
            className="font-semibold text-accent"
          >
            Show more
          </button>
        )}
      </p>
    </div>
  );
}

// Below md each row is two lines: name and family, then members and last
// active -- the same collapse every table in this product makes at 320px.
function HouseholdRow({
  household,
  now,
}: {
  household: AdminHouseholdListing;
  now: Date;
}) {
  const lastActive = relativeTimeLabel(household.lastActiveAt, now);
  return (
    <tr className="border-b border-hairline last:border-b-0 md:table-row">
      <td className="block py-2 pr-3 md:table-cell">
        <Link
          to="/admin/households/$householdId"
          params={{ householdId: household.id }}
          className="font-semibold text-ink hover:text-accent"
        >
          {household.name}
        </Link>
        {household.match && (
          <p className="text-[11.5px] text-muted">
            matched {household.match.memberName}
            {household.match.memberEmail
              ? ` · ${household.match.memberEmail}`
              : ""}
          </p>
        )}
        <span className="text-muted md:hidden"> · {household.familyName}</span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{household.familyName}</td>
      <td className="block pb-2 pr-3 text-muted md:table-cell md:py-2 md:text-ink">
        <span className="md:hidden">{household.memberCount} members · </span>
        <span className="hidden md:inline tabular-nums">
          {household.memberCount}
        </span>
        <span
          className="md:hidden"
          title={
            household.lastActiveAt
              ? exactTimeLabel(household.lastActiveAt)
              : undefined
          }
        >
          {lastActive}
        </span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">
        {dateLabel(household.createdAt)}
      </td>
      <td
        className="hidden py-2 pr-3 md:table-cell"
        title={
          household.lastActiveAt
            ? exactTimeLabel(household.lastActiveAt)
            : undefined
        }
      >
        {lastActive}
      </td>
      <td className="hidden py-2 md:table-cell">{household.primaryCurrency}</td>
    </tr>
  );
}
