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
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignUpScreen } from "./SignUpScreen";

const SIGN_UP_URL = "/api/v1/auth/sign-up";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SignUpScreen", () => {
  it("renders the design's create-household copy", async () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(await screen.findByText("Start your household.")).toBeInTheDocument();
    expect(
      screen.getByText(
        "One household for the whole family. Set it up once, invite your partner, add the kids later.",
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
      [`POST ${SIGN_UP_URL}`]: { status: 202, body: { status: "accepted" } },
    });
    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("Check your email.")).toBeInTheDocument();
    // The panel must describe both outcomes: it cannot know which mail was sent,
    // and must not appear to. Pinned as the full sentence, not just a fragment
    // match -- it is a template literal now (Task 30's fix-round comment nit),
    // so a future edit that changes its wording or drops a clause would
    // otherwise go unnoticed.
    expect(
      screen.getByText(
        "We've sent a link to founder@example.test. If that address already has an account, we've sent sign-in instructions instead. Either way it's good for the next 24 hours.",
      ),
    ).toBeInTheDocument();
  });

  // Fix round 1, Finding 1 (resend path): the sent panel's "Send another
  // link" calls the same handler (submit) as the original send, so a failed
  // resend must surface its own error rather than looking identical to
  // success. The sign-up route is given two responses in order: 202 for the
  // original send, then 500 for the resend.
  it("shows an error in the sent panel when a resend fails", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_UP_URL}`]: [
        { status: 202, body: { status: "accepted" } },
        {
          status: 500,
          body: {
            error: {
              code: "INTERNAL",
              message:
                "Something went wrong. Please try again, or quote this reference if it keeps happening.",
            },
          },
        },
      ],
    });
    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));
    await screen.findByText("Check your email.");

    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));

    expect(
      await screen.findByText(
        "Something went wrong. Please try again, or quote this reference if it keeps happening.",
      ),
    ).toBeInTheDocument();
  });

  // Fix round 1, Finding 1: the resend-failure message must not survive a
  // return to the form -- it belongs to the sent panel, not the screen
  // `onBack` returns to.
  it("clears a stale resend-failure message when returning to the form via 'Use a different address'", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_UP_URL}`]: [
        { status: 202, body: { status: "accepted" } },
        {
          status: 500,
          body: { error: { code: "INTERNAL", message: "Something went wrong." } },
        },
      ],
    });
    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));
    await screen.findByText("Check your email.");

    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));
    await screen.findByText("Something went wrong.");

    fireEvent.click(screen.getByRole("button", { name: "Use a different address" }));

    // Back on the form, with no fresh request made yet -- the old resend
    // failure must not still be showing.
    expect(await screen.findByLabelText("Email")).toBeInTheDocument();
    expect(screen.queryByText("Something went wrong.")).not.toBeInTheDocument();
  });

  // Fix round 1, Finding 1 (interleaving): clearing the error on a `sent`
  // transition (the fix above) only clears an error that already exists at
  // the moment `sent` changes. It does nothing for a request that is still
  // in flight when the user navigates away and only settles afterwards --
  // "Send another link" has no pending guard on "Use a different address",
  // so that interleaving is reachable here exactly the way SignInScreen's
  // identical "Send another link" / "Use a password instead" case is.
  // onError must not render its result anywhere, because by the time it
  // fires the request no longer belongs to the screen it started on.
  //
  // The resend's fetch is a deferred promise rejected manually, so the
  // interleaving is deterministic rather than timing-dependent -- a version
  // that raced real timers could pass by luck and would not pin the fix.
  it("does not apply a stale resend result once the user has navigated back to the form", async () => {
    let rejectResend!: (reason: unknown) => void;
    let signUpCallCount = 0;

    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >((input, init) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const url = String(input);
      if (method === "POST" && url === SIGN_UP_URL) {
        signUpCallCount += 1;
        if (signUpCallCount === 1) {
          // The initial send: resolves immediately so the test can reach
          // the sent panel.
          return Promise.resolve(
            new Response(JSON.stringify({ status: "accepted" }), {
              status: 202,
              headers: { "Content-Type": "application/json" },
            }),
          );
        }
        // The resend: deferred until the test rejects it manually below.
        return new Promise<Response>((_resolve, reject) => {
          rejectResend = reject;
        });
      }
      throw new Error(`unexpected fetch call: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));
    await screen.findByText("Check your email.");

    // Start the resend -- it will not settle until rejectResend() is called.
    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));
    await waitFor(() => expect(signUpCallCount).toBe(2));

    // While it's still in flight, navigate back to the form.
    fireEvent.click(screen.getByRole("button", { name: "Use a different address" }));
    await screen.findByLabelText("Email");

    // Now the abandoned resend fails. Flush a macrotask so every microtask
    // in the mutation's internal chain (fetch rejects -> onError runs) has
    // definitely completed before asserting.
    await act(async () => {
      rejectResend(new TypeError("Failed to fetch"));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // Still on the form, and nothing rendered from the abandoned request --
    // no alert region at all, since this test never submitted anything
    // implausible either.
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // The other half of the same interleaving, and the more user-visible one:
  // an abandoned resend that *succeeds* after "Use a different address" must
  // not yank the user back to the sent panel they just deliberately left.
  // Without the ref guard, onSuccess's unconditional setSent(true) does
  // exactly that.
  it("does not switch back to the sent panel from a stale resend success once the user has navigated back to the form", async () => {
    let resolveResend!: () => void;
    let signUpCallCount = 0;

    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >((input, init) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const url = String(input);
      if (method === "POST" && url === SIGN_UP_URL) {
        signUpCallCount += 1;
        if (signUpCallCount === 1) {
          return Promise.resolve(
            new Response(JSON.stringify({ status: "accepted" }), {
              status: 202,
              headers: { "Content-Type": "application/json" },
            }),
          );
        }
        // The resend: deferred until the test resolves it manually below.
        return new Promise<Response>((resolve) => {
          resolveResend = () =>
            resolve(
              new Response(JSON.stringify({ status: "accepted" }), {
                status: 202,
                headers: { "Content-Type": "application/json" },
              }),
            );
        });
      }
      throw new Error(`unexpected fetch call: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithRouter(<SignUpScreen />);

    const emailInput = await screen.findByLabelText("Email");
    fireEvent.change(emailInput, { target: { value: "founder@example.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Create household" }));
    await screen.findByText("Check your email.");

    // Start the resend -- it will not settle until resolveResend() is called.
    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));
    await waitFor(() => expect(signUpCallCount).toBe(2));

    // While it's still in flight, navigate back to the form.
    fireEvent.click(screen.getByRole("button", { name: "Use a different address" }));
    await screen.findByLabelText("Email");

    // Now the abandoned resend succeeds.
    await act(async () => {
      resolveResend();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // Still on the form -- not bounced back to the sent panel by a request
    // that no longer belongs to the screen the user is looking at.
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.queryByText("Check your email.")).not.toBeInTheDocument();
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
    // stubFetchRoutes({}) registers no route, so a component that posts
    // anyway gets an unhandled rejection from the fetch mock -- caught by
    // react-query's onError, not thrown synchronously. The actual failure
    // signal below is findByText timing out waiting for the validation copy
    // that never appears (it would see the generic API-error fallback
    // instead), not stubFetchRoutes throwing.
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
