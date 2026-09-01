// fireEvent, not @testing-library/user-event: the latter is not a dependency
// anywhere in this codebase (see SignUpScreen.test.tsx's own comment on the
// same choice), so this matches that convention rather than introducing a new
// one for a single file.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignUpCompleteScreen } from "./SignUpCompleteScreen";

const preview = {
  "GET /api/v1/auth/sign-up/tok": {
    status: 200,
    body: { email: "founder@example.test", channel: "email" },
  },
  "GET /api/v1/currencies": {
    status: 200,
    body: {
      currencies: [
        { code: "BRL", symbol: "R$", name: "Brazilian real" },
        { code: "SGD", symbol: "S$", name: "Singapore dollar" },
      ],
    },
  },
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SignUpCompleteScreen", () => {
  it("shows the verified address read-only", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    const email = await screen.findByLabelText("Email");
    expect(email).toHaveValue("founder@example.test");
    // The address is what the token proved. Letting it be edited would mean the
    // form could create an account for an address nobody verified.
    expect(email).toHaveAttribute("readonly");
  });

  it("asks for the design's fields, with its helper text and the corrected hint", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    expect(await screen.findByLabelText("Household name")).toBeInTheDocument();
    expect(
      screen.getByText("Shown at the bottom of the sidebar, beside your name. Change it any time."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Your name")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    // The design says "At least 10 characters"; the codebase enforces 12.
    expect(screen.getByText("At least 12 characters")).toBeInTheDocument();
  });

  it("offers no pre-selected currency", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    // Defaulting to SGD would ship a wrong-currency first impression to
    // everyone who does not notice the field.
    expect(await screen.findByLabelText("Primary currency")).toHaveValue("");
  });

  it("puts the currencies with a symbol in their own group", async () => {
    // Split on the wire's own `symbol`, so adding one server-side promotes a
    // currency here with no frontend change. ALL has none, which is the whole
    // point of the fixture -- `preview`'s two both do.
    stubFetchRoutes({
      ...preview,
      "GET /api/v1/currencies": {
        status: 200,
        body: {
          currencies: [
            { code: "SGD", symbol: "S$", name: "Singapore dollar" },
            { code: "ALL", name: "Albanian lek" },
          ],
        },
      },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    const common = await screen.findByRole("group", { name: "Common" });
    expect(within(common).getByRole("option", { name: /SGD/ })).toBeInTheDocument();
    expect(within(common).queryByRole("option", { name: /ALL/ })).toBeNull();

    const all = screen.getByRole("group", { name: "All currencies" });
    expect(within(all).getByRole("option", { name: /ALL/ })).toBeInTheDocument();
  });

  it("submits every field and enters the app", async () => {
    let posted: unknown = null;
    stubFetchRoutes({
      ...preview,
      "POST /api/v1/auth/sign-up/tok/complete": {
        status: 200,
        body: {
          user: { id: "u1", email: "founder@example.test", displayName: "Ade", avatarInitial: "A" },
          household: {
            id: "h1", name: "Ade & Kris", familyName: "Ade & Kris",
            primaryCurrency: "BRL", showSecondaryCurrency: false,
            secondaryCurrency: "BRL", fxRateMode: "auto",
          },
          membership: {
            id: "m1", householdId: "h1", userId: "u1", role: "owner",
            capabilities: ["calendar", "chores", "money", "marriage"],
          },
          capabilities: ["calendar", "chores", "money", "marriage"],
          spaces: [],
        },
        capture: (body: unknown) => {
          posted = body;
        },
      },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    fireEvent.change(await screen.findByLabelText("Household name"), {
      target: { value: "Ade & Kris" },
    });
    fireEvent.change(screen.getByLabelText("Primary currency"), {
      target: { value: "BRL" },
    });
    fireEvent.change(screen.getByLabelText("Your name"), { target: { value: "Ade" } });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-long-enough-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));

    await waitFor(() => {
      expect(posted).toEqual({
        householdName: "Ade & Kris",
        displayName: "Ade",
        primaryCurrency: "BRL",
        password: "a-long-enough-password",
      });
    });
  });

  it("explains a spent token and points back at sign-up", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 409,
        body: {
          error: {
            code: "SIGNUP_ALREADY_USED",
            // Deliberately different from the component's own copy below --
            // if the component ever fell back to rendering this message
            // instead of branching on `code`, this test must go red, not
            // pass by coincidental overlap between the fixture and the
            // rendered string.
            message: "Backend wording that must never appear on screen.",
          },
        },
      },
      ...{ "GET /api/v1/currencies": preview["GET /api/v1/currencies"] },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    // The exact copy the code branch owns, not the server's message.
    expect(await screen.findByText("This link has already been used.")).toBeInTheDocument();
    expect(
      screen.queryByText("Backend wording that must never appear on screen."),
    ).not.toBeInTheDocument();
    const signIn = screen.getByRole("link", { name: "Sign in" });
    expect(signIn).toHaveAttribute("href", "/sign-in");
  });

  it("explains an expired token", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 410,
        body: {
          error: { code: "TOKEN_EXPIRED", message: "This link has expired or has already been used." },
        },
      },
      ...{ "GET /api/v1/currencies": preview["GET /api/v1/currencies"] },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    expect(
      await screen.findByText("This link has expired. Start again to get a new one."),
    ).toBeInTheDocument();
    const createHousehold = screen.getByRole("link", { name: "Create a household" });
    expect(createHousehold).toHaveAttribute("href", "/sign-up");
  });

  // NOT_FOUND and TOKEN_EXPIRED are a deliberately shared branch: a token
  // that never existed and one that has lapsed must read identically, or the
  // screen would confirm whether a given token was ever issued at all. This
  // pins that the two codes produce the exact same copy and action, not just
  // that TOKEN_EXPIRED alone renders something plausible.
  it("explains a not-found token identically to an expired one", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 404,
        body: {
          error: { code: "NOT_FOUND", message: "No sign-up found for that token." },
        },
      },
      ...{ "GET /api/v1/currencies": preview["GET /api/v1/currencies"] },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    expect(
      await screen.findByText("This link has expired. Start again to get a new one."),
    ).toBeInTheDocument();
    const createHousehold = screen.getByRole("link", { name: "Create a household" });
    expect(createHousehold).toHaveAttribute("href", "/sign-up");
  });

  // A Telegram sign-up has no address to show. Rendering an empty read-only
  // email box would look like a field someone forgot to fill in.
  it("shows the Telegram channel instead of an empty email box", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 200,
        body: { email: "", channel: "telegram" },
      },
      "GET /api/v1/currencies": preview["GET /api/v1/currencies"],
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    await screen.findByText(/telegram/i);
    expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
  });

  it("still shows the read-only address for an email sign-up", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 200,
        body: { email: "someone@example.com", channel: "email" },
      },
      "GET /api/v1/currencies": preview["GET /api/v1/currencies"],
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    expect(
      await screen.findByDisplayValue("someone@example.com"),
    ).toBeInTheDocument();
  });
});
