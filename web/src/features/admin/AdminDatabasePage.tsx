// The operator's database browse. Two components, two routes: the list of
// tables, and one page of one table's rows. Opening a table is a separate
// request on purpose -- it is its own audit row, which is what makes reading
// a household's money a deliberate act with a record rather than the default
// state of a screen.
//
// Nothing here renders a cell as anything but text. Every value on this
// screen is arbitrary database content typed by someone this install has
// never met, so there is no dangerouslySetInnerHTML anywhere in this file and
// there must never be one.
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import type {
  AdminDatabaseColumn,
  AdminDatabaseRows,
  AdminDatabaseTables,
} from "./adminDatabaseSchemas";
import {
  BROWSE_DEFAULT_LIMIT,
  useAdminDatabaseRows,
  useAdminDatabaseTables,
} from "./useAdminDatabase";
import { isNotFound, useCloseSurfaceOnReauth } from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

// The two unavailable states get different copy because they have different
// fixes, and both arrive as a 503 -- so this matches on error.code and never
// on error.status. One means "set the variable and restart"; the other means
// "the variable is set and the connection behind it is down", which is a
// different place to go and a different person to ask. Collapsing them into
// one "unavailable" message would send the operator to fix the wrong thing.
// outboxErrorCopy in AdminMailPage.tsx is the same function for the same
// reason. Anything else falls through to the message the error carries.
function browseErrorCopy(error: unknown): string | null {
  if (!(error instanceof ApiError)) return null;
  if (error.code === "DB_BROWSE_NOT_CONFIGURED") {
    return "The database browse is switched off on this install. Set DATABASE_READONLY_URL and restart the API.";
  }
  if (error.code === "DB_BROWSE_UNAVAILABLE") {
    return "The read-only connection is not answering. Nothing is lost — the reader is: check that Postgres is up and that the hearth_readonly role still exists on it.";
  }
  return error.message;
}

function countLabel(count: number, singular: string): string {
  return `${String(count)} ${singular}${count === 1 ? "" : "s"}`;
}

const ERROR_BOX_CLASS =
  "rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger";

export function AdminDatabasePage() {
  const query = useAdminDatabaseTables();
  useCloseSurfaceOnReauth(query.error);

  // A gate-layer failure (lapsed grant, admin revoked) is about to be
  // replaced by AdminShell's own gate; rendering it inline too would flash
  // a second message for the same failure.
  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
        Database
      </h1>
      <p className="mt-1 text-[13px] text-muted">
        Read-only, one page at a time. Every table opened here is an audit
        row, and columns holding a secret are never selected at all — a
        table&rsquo;s own page explains the markers that stand in for them.
      </p>

      {inlineError && (
        <div role="alert" className={ERROR_BOX_CLASS}>
          {browseErrorCopy(inlineError) ??
            "Something went wrong loading the table list."}
        </div>
      )}

      {query.isPending ? (
        <ListSkeleton testId="database-tables-skeleton" />
      ) : query.data ? (
        <TableList data={query.data} />
      ) : null}
    </PageContainer>
  );
}

function ListSkeleton({ testId }: { testId: string }) {
  return (
    <div
      data-testid={testId}
      aria-hidden="true"
      className="flex flex-col divide-y divide-hairline"
    >
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="h-10 bg-canvas" />
      ))}
    </div>
  );
}

function TableList({ data }: { data: AdminDatabaseTables }) {
  if (data.tables.length === 0) {
    return (
      <p className="text-[13px] text-muted">
        The read-only role can see no tables at all. That is a grant problem,
        not an empty database.
      </p>
    );
  }
  return (
    <ul aria-label="Tables" className="flex flex-col">
      {data.tables.map((table) => (
        <li
          key={table.name}
          className="border-b border-hairline py-2 last:border-b-0"
        >
          {/* search is required, not optional: adminDatabaseTableRoute's own
              validateSearch always returns a concrete {limit, offset}, so a
              link into it needs an explicit starting page -- the first one. */}
          <Link
            to="/admin/database/$table"
            params={{ table: table.name }}
            search={{ limit: BROWSE_DEFAULT_LIMIT, offset: 0 }}
            className="text-[13px] font-semibold text-ink hover:text-accent"
          >
            {table.name}
          </Link>
          <p className="text-[12px] tabular-nums text-muted">
            {countLabel(table.rowCount, "row")} ·{" "}
            {countLabel(table.columns.length, "column")}
          </p>
        </li>
      ))}
    </ul>
  );
}

function BackToTables() {
  return (
    <Link
      to="/admin/database"
      className="text-[12.5px] font-medium text-muted hover:text-ink"
    >
      ‹ Database
    </Link>
  );
}

// The row viewer holds no URL state of its own: table, limit and offset
// arrive as props and paging goes back out through onPage, so the route in
// router.tsx owns the URL -- the same seam AdminHouseholdsPage uses. Here it
// buys more than a working Back button: this route's audit row is the record
// that somebody read a particular page of a particular table, and it can only
// say that if the URL, the request and the screen are the same three numbers.
export function AdminDatabaseTablePage({
  table,
  limit,
  offset,
  onPage,
}: {
  table: string;
  limit: number;
  offset: number;
  onPage: (offset: number) => void;
}) {
  const query = useAdminDatabaseRows(table, limit, offset);
  useCloseSurfaceOnReauth(query.error);

  // Evaluated against query.error itself, never against the gate-filtered
  // inlineError below. isAdminLayerFailure counts NOT_FOUND among the
  // failures AdminGate owns, so inlineError is null for a 404 and
  // isNotFound(inlineError) would be false every single time -- the miss
  // would be swallowed by the filter and this branch would never run.
  //
  // The consequence is a blank page, not a password prompt: nothing on a
  // read path can close the operator surface. useCloseSurfaceOnReauth is the
  // only thing here that invalidates the flags query AdminShell's gate
  // watches, and it fires on ADMIN_REAUTH_REQUIRED alone. So an operator who
  // mistypes a table name in the URL gets the heading and nothing under it,
  // with no error and no data -- a screen that looks broken rather than one
  // that says the table does not exist. Which is why the branch is here and
  // why it reads the raw error; the line's position relative to the const
  // below is not what makes it work.
  if (isNotFound(query.error)) {
    return (
      <PageContainer>
        <BackToTables />
        <p className="text-[13px]">
          There is no table called <code className="font-mono">{table}</code>{" "}
          in this database — check the spelling against the list, or the
          read-only role may not be granted on it.
        </p>
      </PageContainer>
    );
  }

  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <BackToTables />
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
        {table}
      </h1>

      {inlineError && (
        <div role="alert" className={ERROR_BOX_CLASS}>
          {browseErrorCopy(inlineError) ??
            "Something went wrong loading this table."}
        </div>
      )}

      {query.isPending ? (
        <ListSkeleton testId="database-rows-skeleton" />
      ) : query.data ? (
        <TablePanes data={query.data} onPage={onPage} />
      ) : null}
    </PageContainer>
  );
}

// Two panes, stacked below md and side by side above it. A two-column layout
// cannot hold at 305px, which is the narrowest width the operator surface is
// checked at. The columns pane is not a decoration: the rows grid scrolls
// sideways inside its own container, so on a table with thirty columns it is
// the only place the whole schema is legible at once.
function TablePanes({
  data,
  onPage,
}: {
  data: AdminDatabaseRows;
  onPage: (offset: number) => void;
}) {
  return (
    <div className="flex flex-col gap-5 md:flex-row md:items-start">
      <ColumnsPane columns={data.columns} />
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <Legend />
        <RowsTable data={data} />
        <Pager data={data} onPage={onPage} />
      </div>
    </div>
  );
}

function ColumnsPane({ columns }: { columns: AdminDatabaseColumn[] }) {
  return (
    <section
      aria-labelledby="browse-columns-heading"
      className="shrink-0 md:w-[190px]"
    >
      <h2
        id="browse-columns-heading"
        className="text-[11.5px] font-semibold uppercase tracking-[0.04em] text-label"
      >
        Columns
      </h2>
      <ul className="mt-1.5 flex flex-col gap-1">
        {columns.map((column) => (
          <li key={column.name} className="text-[12px] leading-tight">
            <span className="font-medium text-ink">{column.name}</span>
            <span className="block text-muted">
              {column.dataType}
              {/* "redacted", the same word the grid's own header uses and the
                  same word inside the legend's marker -- a third synonym for
                  one concept reads as a fourth thing to work out. */}
              {column.redacted ? " · redacted" : ""}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// The two markers, named in one line above the grid they appear in. Without
// it a reader has no way to tell a value they may not see from a value that
// is not there, and in this schema that difference is sometimes the bug being
// investigated (users.email is NULL for a member who signed in through
// Telegram and never gave an address -- a magic link is SENT to an address,
// so a magic-link member necessarily has one; goal_contributions carries a
// NULL date beside an empty-string note, which is where the two read
// differently on one row).
//
// A redacted column never shows «null», whatever it holds -- the server
// substitutes the marker in the SELECT list rather than fetching the value,
// so NULL never survives to be seen. Do not use users.password_hash as the
// «null» example: it is NULL for every member who has never set a password
// and still renders «redacted» for all of them.
function Legend() {
  return (
    <p data-testid="browse-legend" className="text-[12px] text-muted">
      <code className="font-mono text-ink">«redacted»</code> is a value you may
      not see. <code className="font-mono text-ink">«null»</code> is a value
      that is not there.
    </p>
  );
}

function RowsTable({ data }: { data: AdminDatabaseRows }) {
  if (data.rows.length === 0) {
    return <p className="text-[13px] text-muted">No rows on this page.</p>;
  }
  return (
    // The scroll lives here, on the grid's own container, so a table with
    // thirty columns never makes the page body scroll sideways.
    <div className="overflow-x-auto">
      <table className="w-full text-[12.5px]">
        <thead>
          <tr className="text-left text-[11.5px] font-semibold text-label">
            {data.columns.map((column) => (
              <th
                key={column.name}
                scope="col"
                className="whitespace-nowrap py-1.5 pr-4 align-bottom"
              >
                {column.name}
                {column.redacted && (
                  <span className="block font-normal text-muted">redacted</span>
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.rows.map((row, rowIndex) => (
            <tr
              // No row here carries a key of its own: the browse selects
              // whatever columns a table has, and a primary key is not
              // guaranteed to be among them. The index is stable because
              // this list is replaced wholesale on every page change, never
              // reordered or spliced.
              key={`${String(data.offset)}-${String(rowIndex)}`}
              className="border-b border-hairline last:border-b-0"
            >
              {row.map((cell, cellIndex) => (
                <td
                  key={data.columns[cellIndex]?.name ?? String(cellIndex)}
                  className="whitespace-nowrap py-2 pr-4 font-mono text-[12px] text-ink"
                >
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Paging arithmetic comes from the page the server actually returned, not
// from the props: the service clamps a limit above its own cap, so
// data.limit is what the next offset has to step by if the URL and the screen
// are to keep agreeing about which rows were read.
function Pager({
  data,
  onPage,
}: {
  data: AdminDatabaseRows;
  onPage: (offset: number) => void;
}) {
  const atStart = data.offset <= 0;
  // Counted from the rows actually returned rather than from the limit: on
  // the last page those differ, and this is the one that is true.
  const atEnd = data.offset + data.rows.length >= data.total;
  return (
    <div className="flex flex-wrap items-center gap-3 text-[12px] text-muted">
      <button
        type="button"
        disabled={atStart}
        onClick={() => onPage(Math.max(0, data.offset - data.limit))}
        className="rounded-lg border border-hairline px-2.5 py-1 font-semibold text-ink disabled:cursor-not-allowed disabled:opacity-50"
      >
        Previous
      </button>
      <span className="tabular-nums">
        {data.rows.length === 0
          ? `${countLabel(data.total, "row")} in total`
          : `Rows ${String(data.offset + 1)}–${String(
              data.offset + data.rows.length,
            )} of ${String(data.total)}`}
      </span>
      <button
        type="button"
        disabled={atEnd}
        onClick={() => onPage(data.offset + data.limit)}
        className="rounded-lg border border-hairline px-2.5 py-1 font-semibold text-ink disabled:cursor-not-allowed disabled:opacity-50"
      >
        Next
      </button>
    </div>
  );
}
