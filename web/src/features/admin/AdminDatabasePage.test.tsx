// Follows AdminMailPage.test.tsx: renderWithRouter plus stubFetchRoutes for
// every request, literal strings asserted.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminDatabasePage, AdminDatabaseTablePage } from "./AdminDatabasePage";
import {
  adminDatabaseRowsPath,
  adminDatabaseTablesPath,
  BROWSE_DEFAULT_LIMIT,
} from "./useAdminDatabase";
import type {
  AdminDatabaseRows,
  AdminDatabaseTables,
} from "./adminDatabaseSchemas";

const TABLES_ROUTE = `GET ${adminDatabaseTablesPath()}`;

function tables(): AdminDatabaseTables {
  return {
    tables: [
      {
        name: "users",
        rowCount: 12,
        columns: [
          { name: "id", dataType: "uuid", redacted: false },
          { name: "password_hash", dataType: "text", redacted: true },
        ],
      },
    ],
  };
}

// A page whose second column is withheld and whose third cell is absent --
// the two markers the legend has to explain, on one row.
function rows(overrides: Partial<AdminDatabaseRows> = {}): AdminDatabaseRows {
  return {
    table: "sessions",
    columns: [
      { name: "id", dataType: "uuid", redacted: false },
      { name: "token_hash", dataType: "bytea", redacted: true },
      { name: "revoked_at", dataType: "timestamptz", redacted: false },
    ],
    rows: [["01H8ZK", "«redacted»", "«null»"]],
    total: 1,
    limit: BROWSE_DEFAULT_LIMIT,
    offset: 0,
    ...overrides,
  };
}

function apiError(code: string, message: string) {
  return { error: { code, message } };
}

function renderTable({
  table = "sessions",
  limit = BROWSE_DEFAULT_LIMIT,
  offset = 0,
  onPage = vi.fn(),
}: {
  table?: string;
  limit?: number;
  offset?: number;
  onPage?: (offset: number) => void;
} = {}) {
  return renderWithRouter(
    <AdminDatabaseTablePage
      table={table}
      limit={limit}
      offset={offset}
      onPage={onPage}
    />,
  );
}

describe("AdminDatabasePage", () => {
  it("lists a table by name, with how much is in it", async () => {
    stubFetchRoutes({
      [TABLES_ROUTE]: { status: 200, body: tables() },
    });
    renderWithRouter(<AdminDatabasePage />);

    expect(await screen.findByRole("link", { name: "users" })).toBeInTheDocument();
    expect(screen.getByText("12 rows · 2 columns")).toBeInTheDocument();
  });

  // Matched on error.code, never on error.status === 503: both unavailability
  // answers are 503 and they send the operator to different places.
  it("says the browse is not configured, and names the variable", async () => {
    stubFetchRoutes({
      [TABLES_ROUTE]: {
        status: 503,
        body: apiError(
          "DB_BROWSE_NOT_CONFIGURED",
          "The database browse is not configured.",
        ),
      },
    });
    renderWithRouter(<AdminDatabasePage />);

    expect(
      await screen.findByText(/DATABASE_READONLY_URL/),
    ).toBeInTheDocument();
  });

  // The two 503s are the same status and mean different things: one is an
  // unset variable, the other a connection that is down. One shared
  // "unavailable" message would send the operator to fix the wrong thing.
  it("says a broken connection in different words from an unset variable", async () => {
    stubFetchRoutes({
      [TABLES_ROUTE]: {
        status: 503,
        body: apiError(
          "DB_BROWSE_NOT_CONFIGURED",
          "The database browse is not configured.",
        ),
      },
    });
    const first = renderWithRouter(<AdminDatabasePage />);
    const notConfigured = (await screen.findByRole("alert")).textContent;
    first.unmount();

    stubFetchRoutes({
      [TABLES_ROUTE]: {
        status: 503,
        body: apiError(
          "DB_BROWSE_UNAVAILABLE",
          "The database browse cannot reach its read-only connection.",
        ),
      },
    });
    renderWithRouter(<AdminDatabasePage />);
    const unavailable = (await screen.findByRole("alert")).textContent;

    expect(unavailable).not.toEqual(notConfigured);
    expect(unavailable).not.toMatch(/DATABASE_READONLY_URL/);
  });
});

describe("AdminDatabaseTablePage", () => {
  it("renders a withheld cell as its marker, and says so on the column too", async () => {
    stubFetchRoutes({
      [`GET ${adminDatabaseRowsPath("sessions", BROWSE_DEFAULT_LIMIT, 0)}`]: {
        status: 200,
        body: rows(),
      },
    });
    renderTable();

    expect(
      await screen.findByRole("cell", { name: "«redacted»" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /token_hash/ }),
    ).toHaveTextContent(/redacted/i);
  });

  // A reader who has never seen either marker cannot tell a withheld value
  // from an absent one, and on this screen that difference is sometimes the
  // bug being investigated.
  it("explains both markers in its legend", async () => {
    stubFetchRoutes({
      [`GET ${adminDatabaseRowsPath("sessions", BROWSE_DEFAULT_LIMIT, 0)}`]: {
        status: 200,
        body: rows(),
      },
    });
    renderTable();

    const legend = await screen.findByTestId("browse-legend");
    expect(within(legend).getByText("«redacted»")).toBeInTheDocument();
    expect(within(legend).getByText("«null»")).toBeInTheDocument();
  });

  it("pages forward and back, and refuses to page past either end", async () => {
    // limit 2, not the default: the arithmetic has to come from the page the
    // server actually returned, not from a constant that happens to match.
    stubFetchRoutes({
      [`GET ${adminDatabaseRowsPath("sessions", 2, 0)}`]: {
        status: 200,
        body: rows({
          rows: [
            ["01H8ZK", "«redacted»", "«null»"],
            ["01H8ZL", "«redacted»", "«null»"],
          ],
          total: 5,
          limit: 2,
          offset: 0,
        }),
      },
      [`GET ${adminDatabaseRowsPath("sessions", 2, 4)}`]: {
        status: 200,
        body: rows({
          rows: [["01H8ZP", "«redacted»", "«null»"]],
          total: 5,
          limit: 2,
          offset: 4,
        }),
      },
    });

    const onFirstPage = vi.fn();
    const first = renderTable({ limit: 2, offset: 0, onPage: onFirstPage });

    const nextOnFirst = await screen.findByRole("button", { name: "Next" });
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    fireEvent.click(nextOnFirst);
    await waitFor(() => expect(onFirstPage).toHaveBeenCalledWith(2));
    first.unmount();

    const onLastPage = vi.fn();
    renderTable({ limit: 2, offset: 4, onPage: onLastPage });

    const previousOnLast = await screen.findByRole("button", {
      name: "Previous",
    });
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    fireEvent.click(previousOnLast);
    await waitFor(() => expect(onLastPage).toHaveBeenCalledWith(2));
  });

  // isNotFound is evaluated against the raw query.error. isAdminLayerFailure
  // counts NOT_FOUND, so a 404 filtered through it first would leave this
  // screen with no error to render and no data to render -- a blank page
  // under the heading, which reads as broken rather than as "no such table".
  //
  // This case asserts only what it can: that the copy is on screen. The
  // surface staying open cannot be shown here, because renderWithRouter
  // mounts this component in a one-route tree with no AdminShell and so no
  // AdminGate -- router.test.tsx's "keeps the operator surface open when a
  // browsed table does not exist" mounts the real tree and is where that
  // half is actually proved.
  it("says there is no such table", async () => {
    stubFetchRoutes({
      [`GET ${adminDatabaseRowsPath("sessionz", BROWSE_DEFAULT_LIMIT, 0)}`]: {
        status: 404,
        body: apiError("NOT_FOUND", "That could not be found."),
      },
    });
    renderTable({ table: "sessionz" });

    expect(await screen.findByText(/no table called/i)).toBeInTheDocument();
  });
});
