// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the owner-only PATCH gating and the currency-label
// derivation are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me } from "../auth/schemas";
import { CurrencyPanel } from "./CurrencyPanel";
import type { HouseholdSettings } from "./useHousehold";

const ME_URL = "/api/v1/auth/me";
const HOUSEHOLD_URL = "/api/v1/household";
const CURRENCIES_URL = "/api/v1/currencies";

// CurrencyPanel now calls useCurrencies() unconditionally (it needs the
// served symbol for the non-owner label), so every test's stub must answer
// it -- matching this codebase's rule that every request a component can
// make gets registered, not just the ones a given test happens to assert on.
function currenciesFixture() {
  return {
    currencies: [
      { code: "SGD", symbol: "S$", name: "Singapore dollar" },
      { code: "IDR", symbol: "Rp", name: "Indonesian rupiah" },
      { code: "USD", symbol: "$", name: "US dollar" },
    ],
  };
}

function householdFixture(overrides: Partial<HouseholdSettings> = {}): HouseholdSettings {
  return {
    id: "h-1",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: true,
    secondaryCurrency: "IDR",
    fxRateMode: "auto",
    timezone: "Asia/Singapore",
    primaryCurrencyLocked: false,
    ...overrides,
  };
}

function meFixture(role: "owner" | "limited" = "owner"): Me {
  return {
    user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: householdFixture(),
    membership: {
      id: "mem-1",
      householdId: "h-1",
      userId: "u-1",
      role,
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <CurrencyPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("CurrencyPanel", () => {
  it("renders the primary currency with its symbol for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture() },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    expect(await screen.findByText("SGD (S$)")).toBeInTheDocument();
    expect(screen.getByText("Show IDR equivalents")).toBeInTheDocument();
    // A non-owner gets the plain, non-interactive display -- no input, no
    // way to reach PATCH /household's primaryCurrency field at all.
    expect(screen.queryByLabelText("Primary currency")).not.toBeInTheDocument();
  });

  it("never renders a label pointing at an id that isn't in the DOM for a limited member", async () => {
    // Primary currency's <label htmlFor="primary-currency"> used to render
    // for every member, but the input carrying that id is owner-only
    // (isOwner ? <form><input id="primary-currency" /></form> : <span>) --
    // a limited member got a label pointing at nothing, a DevTools
    // "no form control" accessibility issue.
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture() },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    const { container } = renderPanel();

    await screen.findByText("SGD (S$)");
    // The wording itself must still be on screen for a limited member --
    // this only asserts it stopped being a dangling <label>, not that it
    // disappeared.
    expect(screen.getByText("Primary currency")).toBeInTheDocument();
    container.querySelectorAll("label[for]").forEach((label) => {
      const targetId = label.getAttribute("for");
      expect(document.getElementById(targetId!)).not.toBeNull();
    });
  });

  // QA ISSUE-012. The server refuses a currency change once the household
  // holds investments. An owner used to find that out only after Save.
  it("shows an owner the currency as locked, with the reason, instead of a field to edit", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrencyLocked: true }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    const { container } = renderPanel();

    expect(await screen.findByText("SGD (S$)")).toBeInTheDocument();
    expect(
      screen.getByText("Can't be changed while you hold investments. Every holding records what it cost in this currency."),
    ).toBeInTheDocument();
    // No field and no Save for the currency: nothing on screen can send the
    // request the server would refuse. The time zone's own Save stays.
    expect(screen.queryByLabelText("Primary currency")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save time zone" })).toBeInTheDocument();
    // The wording is still there, as text rather than as a label for a
    // control that is no longer in the page.
    expect(screen.getByText("Primary currency")).toBeInTheDocument();
    container.querySelectorAll("label[for]").forEach((label) => {
      expect(document.getElementById(label.getAttribute("for")!)).not.toBeNull();
    });
    expect(
      fetchMock.mock.calls.some(([, init]) => (init?.method ?? "GET").toUpperCase() === "PATCH"),
    ).toBe(false);
  });

  it("says nothing about a lock to a limited member, who cannot change the currency anyway", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      // The server never sends true to a limited member. The flag is true here
      // on purpose: with false, this test would pass whether or not the panel
      // checks who is looking.
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrencyLocked: true }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    await screen.findByText("SGD (S$)");
    expect(screen.queryByText(/hold investments/)).not.toBeInTheDocument();
  });

  it("keeps the field for an owner whose household holds no investments", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrencyLocked: false }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    expect(await screen.findByLabelText("Primary currency")).toHaveValue("SGD");
    expect(screen.queryByText(/hold investments/)).not.toBeInTheDocument();
  });

  it("issues a PATCH toggling showSecondaryCurrency for an owner", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ showSecondaryCurrency: true }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
      [`PATCH ${HOUSEHOLD_URL}`]: {
        status: 200,
        body: householdFixture({ showSecondaryCurrency: false }),
      },
    });
    renderPanel();

    await screen.findByDisplayValue("SGD");
    fireEvent.click(screen.getByRole("switch", { name: "Show IDR equivalents" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === HOUSEHOLD_URL && (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({ showSecondaryCurrency: false });
    });
  });

  it("disables the toggle for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture() },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    await screen.findByText("SGD (S$)");
    expect(screen.getByRole("switch", { name: "Show IDR equivalents" })).toBeDisabled();
  });

  // Self-serve sign-up stores the primary currency as the second one too,
  // because nothing lets a household choose a second currency yet.
  it("offers no equivalents switch when the second currency is the primary one", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: {
        status: 200,
        body: householdFixture({ primaryCurrency: "SGD", secondaryCurrency: "SGD", showSecondaryCurrency: false }),
      },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    await screen.findByDisplayValue("SGD");
    expect(screen.queryByText(/equivalents/)).not.toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: /equivalents/ })).not.toBeInTheDocument();
    expect(screen.getByText("Second currency")).toBeInTheDocument();
    expect(screen.getByText("None set")).toBeInTheDocument();
  });

  it("offers no equivalents switch when no second currency is stored at all", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: {
        status: 200,
        body: householdFixture({ primaryCurrency: "SGD", secondaryCurrency: "" }),
      },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    await screen.findByDisplayValue("SGD");
    expect(screen.queryByRole("switch", { name: /equivalents/ })).not.toBeInTheDocument();
    expect(screen.getByText("None set")).toBeInTheDocument();
  });

  it("lets an owner edit the primary currency and issues a matching PATCH", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrency: "SGD" }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
      [`PATCH ${HOUSEHOLD_URL}`]: {
        status: 200,
        body: householdFixture({ primaryCurrency: "USD" }),
      },
    });
    renderPanel();

    const input = await screen.findByDisplayValue("SGD");
    // The label keeps pointing at the real input for an owner -- only a
    // non-owner's label became a plain span.
    expect(screen.getByLabelText("Primary currency")).toBe(input);
    fireEvent.change(input, { target: { value: "usd" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([reqInput, init]) =>
          String(reqInput) === HOUSEHOLD_URL && (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      // Lowercase input is uppercased before it ever reaches the request --
      // the backend's own rule (domain.NewMoney) requires uppercase.
      expect(JSON.parse(call![1]!.body as string)).toEqual({ primaryCurrency: "USD" });
    });
  });

  it("keeps Save disabled until the input is exactly three letters and different from the saved value", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrency: "SGD" }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
    });
    renderPanel();

    const input = await screen.findByDisplayValue("SGD");
    const save = screen.getByRole("button", { name: "Save" });

    // Unchanged from the saved value.
    expect(save).toBeDisabled();

    // Too short to be a currency code.
    fireEvent.change(input, { target: { value: "US" } });
    expect(save).toBeDisabled();

    // A real, different, three-letter code enables it.
    fireEvent.change(input, { target: { value: "USD" } });
    expect(save).not.toBeDisabled();
  });

  it("surfaces the backend's own message on a rejected currency code, and keeps the owner's attempted input", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ primaryCurrency: "SGD" }) },
      [`GET ${CURRENCIES_URL}`]: { status: 200, body: currenciesFixture() },
      [`PATCH ${HOUSEHOLD_URL}`]: {
        status: 422,
        body: { error: { code: "INVALID_CURRENCY", message: "That currency code is not valid." } },
      },
    });
    renderPanel();

    const input = await screen.findByDisplayValue("SGD");
    fireEvent.change(input, { target: { value: "ZZZ" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("That currency code is not valid.")).toBeInTheDocument();
    // The rejected attempt stays on screen for the owner to correct, rather
    // than silently reverting to the last saved value.
    expect(screen.getByDisplayValue("ZZZ")).toBeInTheDocument();
  });
});
