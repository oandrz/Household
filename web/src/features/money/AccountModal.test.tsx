import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { AccountModal } from "./AccountModal";

afterEach(() => vi.unstubAllGlobals());

// AccountModal reads the household's primary currency off useMe() (to
// default the Balance currency select for a fresh "add" form) and this
// modal's own tests mount it with a cold QueryClient, so -- like
// FinancesPage.test.tsx -- every scenario here registers GET /auth/me
// explicitly rather than relying on stubFetchRoutes's registered-or-throw
// default to happen to not matter.
function meFixture(): Me {
  return {
    user: { id: "u1", email: "a@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
  };
}

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// GET /household/members answers a bare JSON array (member_handlers.go's
// handleListMembers writes `out`, a []memberViewDTO, straight to the
// response -- there is no wrapping object), matching membersListSchema's
// z.array(memberSchema) on this side.
const NO_MEMBERS = { status: 200, body: [] };

describe("AccountModal", () => {
  it("posts what the form was given, in minor units", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
      "POST /api/v1/accounts": {
        status: 200,
        body: {
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { posted = body; },
      },
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    // renderWithRouter's RouterProvider does not mount its route synchronously
    // -- even a trivial root route goes through an async pending state, so
    // nothing in this tree exists yet on the very next line without an await.
    // findByLabelText both waits that out and gets the first field.
    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "DBS Everyday" } });
    fireEvent.change(screen.getByLabelText("Balance"), { target: { value: "8240.55" } });
    // The currency select defaults to the household's primary once useMe()
    // resolves -- a second, independent async gap this test has to wait out
    // (unlike the debt test below), because it is the one assertion here that
    // actually depends on the value that default produces. Firing the click
    // before this resolves would post whatever the select's initial, pre-load
    // value was instead.
    await waitFor(() => expect(screen.getByLabelText("Currency")).toHaveValue("SGD"));
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    await waitFor(() => expect(posted).toBeDefined());
    expect(posted).toMatchObject({
      nickname: "DBS Everyday",
      type: "cash",
      openingBalanceMinor: 824055,
      openingBalanceCurrency: "SGD",
    });
  });

  // The rule that stops a car loan from making a household look richer, said
  // in the form rather than only in a 422. The backend refuses it either way.
  it("tells you to enter a debt as a positive amount", async () => {
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "Car loan" } });
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "loan" } });
    fireEvent.change(screen.getByLabelText("Balance"), { target: { value: "-14500" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    expect(
      await screen.findByText(/Enter what you owe as a positive amount/),
    ).toBeInTheDocument();
    // The field error has to stop the request, not just accompany it -- if
    // this fired the POST anyway, an unregistered route would turn into a
    // mutation error whose message could coincidentally satisfy the assertion
    // above without the guard actually having done anything.
    expect(
      fetchMock.mock.calls.some(([input]) => String(input) === "/api/v1/accounts"),
    ).toBe(false);
  });

  it("tells you to enter a number when the balance can't be parsed", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "Car loan" } });
    fireEvent.change(screen.getByLabelText("Balance"), { target: { value: "eight" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    // Distinct copy from the debt message above: "not a number" and "that
    // number has the wrong sign" are different mistakes.
    expect(await screen.findByText("Enter an amount, like 8240.55.")).toBeInTheDocument();
  });

  it("prefills an existing account and PATCHes the change, clearing Owner to Shared", async () => {
    let patched: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": {
        status: 200,
        body: [{ id: "m2", user: { id: "u2", email: "", displayName: "Christine", avatarInitial: "C" }, role: "owner", capabilities: [] }],
      },
      "PATCH /api/v1/accounts/a1": {
        status: 200,
        body: {
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { patched = body; },
      },
    });

    renderWithRouter(
      <AccountModal
        open
        onClose={() => {}}
        account={{
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: "m2", ownerName: "Christine",
          balance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        }}
      />,
    );

    expect(await screen.findByLabelText("Nickname")).toHaveValue("DBS Everyday");
    expect(screen.getByLabelText("Balance")).toHaveValue("8240.55");
    // The Owner select's own <option>s only exist once the household members
    // list resolves -- the field's *value* (this component's own state) is
    // already "m2" on first render, but the option to display it against
    // arrives on its own, independent async gap.
    await waitFor(() => expect(screen.getByLabelText("Owner")).toHaveValue("m2"));
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Owner"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patched).toBeDefined());
    // updateAccountRequest.OwnerMembershipID is nil-means-unchanged
    // (account_handlers.go): posting JSON null here would leave Christine as
    // the owner. Only "" actually clears it to Shared.
    expect(patched).toMatchObject({ ownerMembershipId: "" });
  });
});
