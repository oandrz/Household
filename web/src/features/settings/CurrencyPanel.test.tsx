// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the owner-only PATCH gating and the currency-label
// derivation are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Household, Me } from "../auth/schemas";
import { CurrencyPanel } from "./CurrencyPanel";

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

function householdFixture(overrides: Partial<Household> = {}): Household {
  return {
    id: "h-1",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: true,
    secondaryCurrency: "IDR",
    fxRateMode: "auto",
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
    fireEvent.click(screen.getByRole("switch", { name: /show secondary currency/i }));

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
    expect(screen.getByRole("switch", { name: /show secondary currency/i })).toBeDisabled();
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
    fireEvent.change(input, { target: { value: "usd" } });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

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
    const save = screen.getByRole("button", { name: /save/i });

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
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText("That currency code is not valid.")).toBeInTheDocument();
    // The rejected attempt stays on screen for the owner to correct, rather
    // than silently reverting to the last saved value.
    expect(screen.getByDisplayValue("ZZZ")).toBeInTheDocument();
  });
});
