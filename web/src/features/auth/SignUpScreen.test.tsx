// fireEvent, not @testing-library/user-event: the latter is not a dependency
// anywhere in this codebase (SignInScreen.test.tsx and InviteScreen.test.tsx
// both drive forms with fireEvent), so this matches that convention rather
// than introducing a new one for a single file.
//
// The first assertion in every test here is an awaited `find*`, not a plain
// `getBy*`, matching every other renderWithRouter-based test in this codebase
// (see Sidebar.test.tsx) -- RouterProvider's own initial match/transition
// hasn't necessarily settled synchronously by the time `render()` returns,
// even though SignUpScreen itself makes no request on mount.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignUpScreen } from "./SignUpScreen";

describe("SignUpScreen", () => {
  it("renders the design's create-household copy", async () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(await screen.findByText("Start your household.")).toBeInTheDocument();
    expect(
      screen.getByText(
        "One household, two owners. Set it up once and invite your partner in.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create household" })).toBeInTheDocument();
  });

  // The design's own footer link back into sign-in ("Already set up? Sign
  // in"), verbatim like the rest of this screen's copy.
  it("links to /sign-in from the 'Already set up?' footer", async () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    await screen.findByText("Start your household.");
    expect(screen.getByText("Already set up?")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/sign-in",
    );
  });

  // The design gates all three on authNotCreate.
  it("hides the magic-link controls the design hides in this state", async () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    await screen.findByText("Start your household.");
    expect(screen.queryByRole("button", { name: "Forgot?" })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Email me a one-time sign-in link" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("or")).not.toBeInTheDocument();
  });

  it("asks only for an email address at this step", async () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(await screen.findByLabelText("Email")).toBeInTheDocument();
    // Household name, your name and password come after the link is clicked --
    // nothing is stored until the address is verified.
    expect(screen.queryByLabelText("Household name")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("shows the sent panel after a successful request", async () => {
    const fetchMock = stubFetchRoutes({
      "POST /api/v1/auth/sign-up": { status: 202, body: { status: "accepted" } },
    });
    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("Check your email.")).toBeInTheDocument();
    // The panel must describe both outcomes: it cannot know which mail was sent,
    // and must not appear to.
    expect(screen.getByText(/already has an account/i)).toBeInTheDocument();
  });

  // fireEvent.submit(form), not fireEvent.click(the submit button): jsdom's
  // own HTML5 constraint validation (required + type="email" on the input)
  // blocks a *click*-triggered submission of "not-an-email" before the
  // "submit" event ever fires -- the same thing a real browser would do,
  // which would make this test pass for a reason that has nothing to do with
  // the component's own guard. Dispatching "submit" directly on the form
  // exercises handleSubmit the way the panel's "Send another link" (outside
  // any <form>) already has to: through this exact validation branch, not
  // through the browser's.
  it("does not post an implausible address", async () => {
    // stubFetchRoutes throws on an unregistered request, so registering nothing
    // is the assertion: if the component posts, the test fails.
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "not-an-email" } });
    fireEvent.submit(emailInput.closest("form")!);
    expect(
      await screen.findByText("Enter your email address to create a household."),
    ).toBeInTheDocument();
  });
});
