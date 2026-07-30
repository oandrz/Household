// Follows AccountModal.test.tsx/TransactionModal.test.tsx's conventions:
// renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered), fireEvent + findBy*/waitFor
// for the async gaps a real mount always has.
//
// BudgetModal calls `useBudget(month)` itself (see BudgetModal.tsx's own
// header comment on why), so every test here has to stub
// `GET /api/v1/budgets/<month>` even though the component under test never
// takes that as a prop -- the same shape useBudget.test.ts's own tests take.
// It also always fetches the archived-inclusive category list
// (`GET /api/v1/categories?includeArchived=true`) for the archived-name
// gotcha check, so that is stubbed everywhere too.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BudgetModal } from "./BudgetModal";
import type { BudgetMonthResponse } from "./budgetSchemas";
import type { TemplatePrefill } from "./budgetTemplates";

afterEach(() => {
  vi.unstubAllGlobals();
});

const MONTH = "2026-07";

const CATEGORIES = [
  { id: "cat-1", name: "Groceries", kind: "expense" as const },
  { id: "cat-2", name: "Dining out", kind: "expense" as const },
];

function budgetFixture(overrides: Partial<BudgetMonthResponse> = {}): BudgetMonthResponse {
  return {
    currency: "SGD",
    month: MONTH,
    budget: { expectedIncomeMinor: 500000, lines: [{ categoryId: "cat-1", capMinor: 80000 }] },
    categories: [
      { categoryId: "cat-1", name: "Groceries", archived: false, capMinor: 80000, spentMinor: 0, over: false },
    ],
    budgetedMinor: 80000,
    spentMinor: 0,
    remainingMinor: 80000,
    percentUsed: 0,
    percentOk: true,
    daysLeft: 10,
    dailyPaceMinor: 0,
    dailyPaceOk: true,
    byPerson: [],
    excludedNoRate: 0,
    overCount: 0,
    ...overrides,
  };
}

const DEFAULT_INITIAL: TemplatePrefill = {
  expectedIncomeMinor: 500000,
  lines: [{ categoryId: "cat-1", capMinor: 80000 }],
  missing: [],
};

// The archived-inclusive endpoint's default stub: in the real API this is
// the household's *whole* roster (active + archived), not archived rows
// alone, so a fixture representing "nothing is archived yet" has to still
// carry the same active categories `CATEGORIES` above does -- buildRows
// resolves every row's display name off this list (Defect A's fix), and a
// bare `{ categories: [] }` would make every default-fixture row read as
// "Unknown category" the same way the real bug did.
const NO_ARCHIVED = {
  status: 200,
  body: { categories: CATEGORIES.map((c) => ({ ...c, archived: false })) },
};

function renderModal(
  props: Partial<Parameters<typeof BudgetModal>[0]> = {},
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  const fetchMock = stubFetchRoutes({
    [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetFixture() },
    "GET /api/v1/categories?includeArchived=true": NO_ARCHIVED,
    ...extraRoutes,
  });

  renderWithRouter(
    <BudgetModal
      month={MONTH}
      initial={DEFAULT_INITIAL}
      categories={CATEGORIES}
      awaitingIncome={false}
      onClose={onClose}
      onSaved={onSaved}
      {...props}
    />,
  );

  return { fetchMock, onClose, onSaved };
}

// Only the writes matter for order assertions -- the two GETs every test
// fires (the budget month, the archived-inclusive category list) are noise
// the pinned "creates/renames/archives run first, then one PUT" ordering
// doesn't care about.
function mutatingCalls(fetchMock: ReturnType<typeof stubFetchRoutes>): string[] {
  return fetchMock.mock.calls
    .filter(([, init]) => (init?.method ?? "GET").toUpperCase() !== "GET")
    .map(([input, init]) => `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`);
}

describe("BudgetModal", () => {
  it("renders existing lines with their real names and caps", async () => {
    renderModal();

    await screen.findByLabelText("Expected income");
    const row = screen.getByTestId("budget-modal-row-cat-1");
    expect(within(row).getByDisplayValue("Groceries")).toBeInTheDocument();
    expect(within(row).getByDisplayValue("800.00")).toBeInTheDocument();
  });

  it("saves queued creates before the one PUT, in that order", async () => {
    let postBody: unknown;
    let putBody: unknown;
    const { fetchMock, onSaved, onClose } = renderModal(undefined, {
      "POST /api/v1/categories": {
        status: 201,
        body: { category: { id: "cat-new", name: "Rent", kind: "expense", archived: false } },
        capture: (body) => {
          postBody = body;
        },
      },
      [`PUT /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: { budget: { expectedIncomeMinor: 500000, lines: [] } },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    await screen.findByLabelText("Expected income");
    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Rent" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(postBody).toEqual({ name: "Rent" });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/categories", `PUT /api/v1/budgets/${MONTH}`]);
    expect(putBody).toMatchObject({
      lines: expect.arrayContaining([{ categoryId: "cat-new", capMinor: 0 }]),
    });
  });

  it("keeps the modal open and never fires the PUT when a create 409s on a taken name", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/categories": {
        status: 409,
        body: { error: { code: "CATEGORY_NAME_TAKEN", message: "A category with that name already exists." } },
      },
    });

    await screen.findByLabelText("Expected income");
    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Rent" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));
    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    expect(await screen.findByRole("alert")).toHaveTextContent('"Rent" is already a category name in this household.');
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/categories"]);
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
  });

  // The single-item 409 test above only proves the *first* queued write can
  // stop the PUT. This proves the halt also stops every write *behind* the
  // failing one: a queued rename on row 1 that 409s must never let row 2's
  // queued archive fire, even though nothing about row 2 itself is invalid.
  it("stops at the first failure in a multi-item queue -- a later row's queued archive never fires", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(
      {
        initial: {
          expectedIncomeMinor: 500000,
          lines: [
            { categoryId: "cat-1", capMinor: 80000 },
            { categoryId: "cat-2", capMinor: 45000 },
          ],
          missing: [],
        },
      },
      {
        "PATCH /api/v1/categories/cat-1": {
          status: 409,
          body: { error: { code: "CATEGORY_NAME_TAKEN", message: "A category with that name already exists." } },
        },
        // Registered (not left unregistered) so that if the halt regresses,
        // the archive actually fires and shows up in `mutatingCalls` as a
        // clean assertion failure rather than an unrelated stub-not-found
        // rejection masking the real bug.
        "POST /api/v1/categories/cat-2/archive": {
          status: 200,
          body: { category: { id: "cat-2", name: "Dining out", kind: "expense", archived: true } },
        },
        [`PUT /api/v1/budgets/${MONTH}`]: { status: 200, body: { budget: { expectedIncomeMinor: 500000, lines: [] } } },
      },
    );

    await screen.findByLabelText("Expected income");
    const firstRow = screen.getByTestId("budget-modal-row-cat-1");
    fireEvent.change(within(firstRow).getByDisplayValue("Groceries"), { target: { value: "Food" } });
    const secondRow = screen.getByTestId("budget-modal-row-cat-2");
    fireEvent.click(within(secondRow).getByRole("button", { name: "Archive" }));

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    expect(await screen.findByRole("alert")).toHaveTextContent('"Food" is already a category name in this household.');
    expect(mutatingCalls(fetchMock)).toEqual(["PATCH /api/v1/categories/cat-1"]);
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("drops a row removed with the ✕ control from the PUT body", async () => {
    let putBody: unknown;
    renderModal(
      {
        initial: {
          expectedIncomeMinor: 500000,
          lines: [
            { categoryId: "cat-1", capMinor: 80000 },
            { categoryId: "cat-2", capMinor: 45000 },
          ],
          missing: [],
        },
      },
      {
        [`PUT /api/v1/budgets/${MONTH}`]: {
          status: 200,
          body: { budget: { expectedIncomeMinor: 500000, lines: [] } },
          capture: (body) => {
            putBody = body;
          },
        },
      },
    );

    await screen.findByLabelText("Expected income");
    const diningRow = screen.getByTestId("budget-modal-row-cat-2");
    fireEvent.click(within(diningRow).getByRole("button", { name: "Remove" }));
    expect(screen.queryByTestId("budget-modal-row-cat-2")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(putBody).toBeDefined());
    expect(putBody).toEqual({
      expectedIncomeMinor: 500000,
      lines: [{ categoryId: "cat-1", capMinor: 80000 }],
    });
  });

  it("sends expectedIncomeMinor: null and hides the allocated/left-to-allocate cards when income is blanked", async () => {
    let putBody: unknown;
    renderModal(undefined, {
      [`PUT /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: { budget: { expectedIncomeMinor: null, lines: [] } },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    await screen.findByLabelText("Expected income");
    expect(screen.getByTestId("budget-modal-allocated")).toBeInTheDocument();
    expect(screen.getByTestId("budget-modal-left-to-allocate")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Expected income"), { target: { value: "" } });
    expect(screen.queryByTestId("budget-modal-allocated")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-modal-left-to-allocate")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(putBody).toBeDefined());
    expect(putBody).toMatchObject({ expectedIncomeMinor: null });
  });

  it("excludes already-capped and archived categories from the add-category dropdown", async () => {
    renderModal({
      categories: [
        { id: "cat-1", name: "Groceries", kind: "expense" }, // already capped via DEFAULT_INITIAL
        { id: "cat-2", name: "Dining out", kind: "expense" }, // available
        { id: "cat-3", name: "Old hobby", kind: "expense", archived: true }, // archived
      ],
    });

    await screen.findByLabelText("Expected income");
    const select = screen.getByLabelText("+ Add a category");
    expect(within(select).queryByRole("option", { name: "Dining out" })).toBeInTheDocument();
    expect(within(select).queryByRole("option", { name: "Groceries" })).not.toBeInTheDocument();
    expect(within(select).queryByRole("option", { name: "Old hobby" })).not.toBeInTheDocument();
  });

  it("PATCHes a renamed existing category before the PUT", async () => {
    let patchBody: unknown;
    const { fetchMock } = renderModal(undefined, {
      "PATCH /api/v1/categories/cat-1": {
        status: 200,
        body: { category: { id: "cat-1", name: "Food", kind: "expense", archived: false } },
        capture: (body) => {
          patchBody = body;
        },
      },
      [`PUT /api/v1/budgets/${MONTH}`]: { status: 200, body: { budget: { expectedIncomeMinor: 500000, lines: [] } } },
    });

    await screen.findByLabelText("Expected income");
    const row = screen.getByTestId("budget-modal-row-cat-1");
    fireEvent.change(within(row).getByDisplayValue("Groceries"), { target: { value: "Food" } });
    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toEqual({ name: "Food" });
    expect(mutatingCalls(fetchMock)).toEqual(["PATCH /api/v1/categories/cat-1", `PUT /api/v1/budgets/${MONTH}`]);
  });

  // Regression: a line whose category isn't in the active `categories` prop
  // (e.g. "Import last month" handing through a category archived since --
  // BudgetPage.tsx's own comment says that handoff carries real ids
  // unchanged, with no name-mapping) falls back to a placeholder display
  // name. That fallback must never itself look like a rename: the row's
  // `name` and `originalName` need to start out equal, or every save on an
  // unresolved row queues a PATCH renaming a real category to the
  // placeholder string.
  it("never renames a category it can't name -- the unresolved-line fallback isn't a queued rename", async () => {
    const { fetchMock } = renderModal(
      {
        initial: {
          expectedIncomeMinor: 500000,
          lines: [{ categoryId: "cat-gone", capMinor: 10000 }],
          missing: [],
        },
        categories: [], // cat-gone is not in the active list this modal was handed
      },
      {
        [`PUT /api/v1/budgets/${MONTH}`]: { status: 200, body: { budget: { expectedIncomeMinor: 500000, lines: [] } } },
      },
    );

    await screen.findByLabelText("Expected income");
    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(mutatingCalls(fetchMock).length).toBeGreaterThan(0));
    expect(mutatingCalls(fetchMock)).toEqual([`PUT /api/v1/budgets/${MONTH}`]);
  });

  // formatMoney renders a negative figure with U+2212 MINUS, not a hyphen --
  // asserting against "-" here would pass vacuously even if that glyph were
  // ever accidentally dropped.
  it("shows Left to allocate as negative (U+2212 MINUS) when caps exceed income", async () => {
    renderModal({
      initial: {
        expectedIncomeMinor: 50000,
        lines: [{ categoryId: "cat-1", capMinor: 80000 }],
        missing: [],
      },
    });

    const leftToAllocate = await screen.findByTestId("budget-modal-left-to-allocate");
    expect(leftToAllocate).toHaveTextContent("−");
    expect(leftToAllocate).toHaveTextContent("SGD 300.00");
  });

  // One save combining all three ways a row can get into the PUT's line
  // set: a survivor from `initial` left untouched, an existing category
  // picked from the dropdown (no network write needed at all), and a
  // brand-new category created on save -- all landing in the one PUT body
  // together, each with its own real cap.
  it("combines a survivor, a dropdown-picked category and a new create in one PUT body", async () => {
    let postBody: unknown;
    let putBody: unknown;
    const { fetchMock, onSaved } = renderModal(
      {
        categories: [
          { id: "cat-1", name: "Groceries", kind: "expense" },
          { id: "cat-2", name: "Dining out", kind: "expense" },
        ],
      },
      {
        "POST /api/v1/categories": {
          status: 201,
          body: { category: { id: "cat-new", name: "Rent", kind: "expense", archived: false } },
          capture: (body) => {
            postBody = body;
          },
        },
        [`PUT /api/v1/budgets/${MONTH}`]: {
          status: 200,
          body: { budget: { expectedIncomeMinor: 500000, lines: [] } },
          capture: (body) => {
            putBody = body;
          },
        },
      },
    );

    await screen.findByLabelText("Expected income");
    // cat-1 (Groceries) is the survivor from DEFAULT_INITIAL, cap 80000 --
    // left untouched.

    // Pick cat-2 (Dining out) straight off the dropdown -- an existing,
    // active category with no cap yet this month, no network write required.
    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "cat-2" } });
    fireEvent.change(within(screen.getByTestId("budget-modal-row-cat-2")).getByLabelText("Cap"), {
      target: { value: "450" },
    });

    // Create a brand-new category.
    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Rent" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));
    const newRowByName = screen.getAllByTestId(/^budget-modal-row-new-/)[0];
    fireEvent.change(within(newRowByName).getByLabelText("Cap"), { target: { value: "200" } });

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(postBody).toEqual({ name: "Rent" });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/categories", `PUT /api/v1/budgets/${MONTH}`]);
    expect(putBody).toMatchObject({
      lines: expect.arrayContaining([
        { categoryId: "cat-1", capMinor: 80000 },
        { categoryId: "cat-2", capMinor: 45000 },
        { categoryId: "cat-new", capMinor: 20000 },
      ]),
    });
    expect((putBody as { lines: unknown[] }).lines).toHaveLength(3);
  });

  it("Cancel discards everything queued -- no write is ever fired", async () => {
    const { fetchMock, onClose } = renderModal();

    await screen.findByLabelText("Expected income");
    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Rent" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onClose).toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual([]);
  });

  // The archived-name gotcha (ledgered from Task 13's review): a name that
  // looks brand-new to the active category list might belong to an ARCHIVED
  // one -- creating it would 409 on categories_household_id_name_key. This
  // pins that "Add" resolves to a restore, not a create, when that's true.
  it("offers restore instead of create when a typed name matches an archived category", async () => {
    let restoreCalled = false;
    let putBody: unknown;
    const { fetchMock, onSaved } = renderModal(undefined, {
      "GET /api/v1/categories?includeArchived=true": {
        status: 200,
        body: { categories: [{ id: "cat-archived-1", name: "Health", kind: "expense", archived: true }] },
      },
      "POST /api/v1/categories/cat-archived-1/restore": {
        status: 200,
        body: { category: { id: "cat-archived-1", name: "Health", kind: "expense", archived: false } },
        capture: () => {
          restoreCalled = true;
        },
      },
      [`PUT /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: { budget: { expectedIncomeMinor: 500000, lines: [] } },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    await screen.findByLabelText("Expected income");
    // Wait for the archived-inclusive fetch to actually land in state before
    // acting -- addCategoryByName reads its result synchronously on click,
    // so acting before it resolves would silently fall back to "create".
    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([input]) => String(input).includes("includeArchived=true"))).toBe(true),
    );
    await waitFor(() => expect(screen.queryByText(/loading/i)).not.toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Health" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));

    const row = await screen.findByTestId("budget-modal-row-cat-archived-1");
    expect(row).toHaveTextContent("Archived");
    fireEvent.change(within(row).getByLabelText("Cap"), { target: { value: "500" } });

    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(restoreCalled).toBe(true);
    expect(mutatingCalls(fetchMock)).toEqual([
      "POST /api/v1/categories/cat-archived-1/restore",
      `PUT /api/v1/budgets/${MONTH}`,
    ]);
    // The restored row's cap has to actually land in the PUT's line set --
    // a restore that fires but never makes it into the budget would leave
    // the household clicking "Add" for nothing.
    expect(putBody).toMatchObject({
      lines: expect.arrayContaining([{ categoryId: "cat-archived-1", capMinor: 50000 }]),
    });
  });

  // Task 17 browser walk, Defect A: reopening Edit budget for a line whose
  // category was archived since (the queue-archive flow saved it) rendered
  // "Unknown category" instead of the real name -- BudgetCategoryGrid gets
  // this right (it reads useBudget's own per-month `categories`, which
  // always carries archived rows), but buildRows here was resolving names
  // off the active-only `categories` prop, which by construction excludes
  // exactly the category this line points at. The fix has to read the
  // archived-inclusive list (already fetched for the archived-name gotcha
  // below) for name resolution, while the add-dropdown keeps using the
  // active-only prop.
  it("names an archived category's line with its real name and the archived marker, not the unresolved-line fallback", async () => {
    const { fetchMock } = renderModal(
      {
        initial: {
          expectedIncomeMinor: 500000,
          lines: [{ categoryId: "cat-petrol", capMinor: 12000 }],
          missing: [],
        },
        categories: [], // active-only prop: Petrol is archived, so it's excluded here
      },
      {
        "GET /api/v1/categories?includeArchived=true": {
          status: 200,
          body: { categories: [{ id: "cat-petrol", name: "Petrol", kind: "expense", archived: true }] },
        },
        [`PUT /api/v1/budgets/${MONTH}`]: { status: 200, body: { budget: { expectedIncomeMinor: 500000, lines: [] } } },
      },
    );

    const row = await screen.findByTestId("budget-modal-row-cat-petrol");
    expect(within(row).getByDisplayValue("Petrol")).toBeInTheDocument();
    expect(row).toHaveTextContent("(archived)");
    expect(screen.queryByText(/Unknown category/i)).not.toBeInTheDocument();

    // Saving must queue no rename -- the row's name and originalName both
    // resolved to "Petrol" from the same source, so this isn't a rename.
    fireEvent.click(screen.getByRole("button", { name: "Save budget" }));
    await waitFor(() => expect(mutatingCalls(fetchMock).length).toBeGreaterThan(0));
    expect(mutatingCalls(fetchMock)).toEqual([`PUT /api/v1/budgets/${MONTH}`]);
  });

  // Task 17 browser walk, Defect B: typing an already-used name into "New
  // category…" and clicking Add was a silent no-op -- the client-side
  // duplicate guard (`rows.some((row) => row.name === name)`) returned with
  // no row added and no feedback of any kind, not even a console warning.
  // The fix keeps the same guard (same comparison, not touching matching
  // semantics) but surfaces it as a visible inline error next to the add
  // control, reusing the 409 CATEGORY_NAME_TAKEN copy shape.
  it("shows an inline error and adds no row when the typed name duplicates a row already in the modal", async () => {
    const { fetchMock } = renderModal(undefined, {
      "POST /api/v1/categories": {
        status: 201,
        body: { category: { id: "cat-should-not-be-created", name: "Groceries", kind: "expense", archived: false } },
      },
    });

    await screen.findByLabelText("Expected income");
    const rowsBefore = screen.getAllByTestId(/^budget-modal-row-/).length;

    fireEvent.change(screen.getByLabelText("+ Add a category"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("New category name"), { target: { value: "Groceries" } });
    fireEvent.click(within(screen.getByTestId("budget-modal-new-category-form")).getByRole("button", { name: "Add" }));

    expect(await screen.findByText('"Groceries" is already a category name in this household.')).toBeInTheDocument();
    expect(screen.getAllByTestId(/^budget-modal-row-/)).toHaveLength(rowsBefore);
    expect((screen.getByLabelText("New category name") as HTMLInputElement).value).toBe("Groceries");
    // Registered above so a regression shows as a clean assertion failure
    // (an unexpected write in the list) rather than a stub-not-found
    // rejection masking the real bug -- the house convention (see the
    // multi-item-queue test's own comment on why).
    expect(mutatingCalls(fetchMock)).toEqual([]);
  });

  describe("the 50/30/20 waiting-for-income state", () => {
    it("focuses the income field on open", async () => {
      renderModal({
        awaitingIncome: true,
        initial: { expectedIncomeMinor: null, lines: [], missing: [] },
      });

      const incomeInput = await screen.findByLabelText("Expected income");
      expect(incomeInput).toHaveFocus();
    });

    it("shows no lines while income is blank, then computes lines summing to at most income once it's entered", async () => {
      renderModal({
        awaitingIncome: true,
        initial: { expectedIncomeMinor: null, lines: [], missing: [] },
        categories: [
          { id: "cat-1", name: "Groceries", kind: "expense" },
          { id: "cat-2", name: "Dining out", kind: "expense" },
        ],
      });

      await screen.findByLabelText("Expected income");
      expect(screen.getByTestId("budget-modal-fifty-thirty-twenty-prompt")).toBeInTheDocument();
      expect(screen.queryAllByTestId(/^budget-modal-row-/)).toHaveLength(0);

      fireEvent.change(screen.getByLabelText("Expected income"), { target: { value: "10000" } });

      const rows = await screen.findAllByTestId(/^budget-modal-row-/);
      expect(rows.length).toBeGreaterThan(0);
      expect(screen.queryByTestId("budget-modal-fifty-thirty-twenty-prompt")).not.toBeInTheDocument();

      // Every computed line's cap is read straight off its input and summed
      // in major units (SGD, 2 decimals) -- the template's own tests already
      // pin the exact per-line math (budgetTemplates.test.ts); this only
      // pins that the modal actually wires that live, not a second copy of
      // the arithmetic.
      const capInputs = screen.getAllByLabelText("Cap") as HTMLInputElement[];
      const sumMajor = capInputs.reduce((sum, input) => sum + (Number(input.value) || 0), 0);
      // Income was entered as "10000" (S$10,000.00) above -- the sum of
      // computed caps must never exceed the major-unit figure actually typed.
      expect(sumMajor).toBeLessThanOrEqual(10000);
      expect(sumMajor).toBeGreaterThan(0);
    });
  });
});
