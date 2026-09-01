import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignInScreen } from "./SignInScreen";

const SIGN_IN_URL = "/api/v1/auth/sign-in";
const MAGIC_LINK_URL = "/api/v1/auth/magic-link";
const TELEGRAM_START_URL = "/api/v1/auth/telegram/start";

// Mirrors api/internal/adapter/http/auth_handlers.go's meResponseBody -- a
// successful sign-in answers with this shape.
const meBody = {
  user: {
    id: "u-andreas",
    email: "andreas@hearth.family",
    displayName: "Andreas",
    avatarInitial: "A",
  },
  household: {
    id: "h-oentoro",
    name: "Oentoro household",
    familyName: "Andreas & Christine",
    primaryCurrency: "SGD",
    showSecondaryCurrency: true,
    secondaryCurrency: "IDR",
    fxRateMode: "auto",
  },
  membership: {
    id: "m-andreas",
    householdId: "h-oentoro",
    userId: "u-andreas",
    role: "owner",
    capabilities: ["calendar", "chores", "money", "marriage"],
  },
  capabilities: ["calendar", "chores", "money", "marriage"],
  spaces: [],
};

// Wrapped in a router (Task 30 added a <Link to="/sign-up"> footer to
// SignInScreen, which throws outside any router context) and, unlike
// renderWithRouter, awaited internally rather than at every call site: a bare
// synchronous render() here leaves RouterProvider's own initial match/
// transition unresolved (its internal Transitioner component updates state
// after mount), so the very next synchronous getByText/fireEvent call in most
// of this file's existing tests would hit an empty document. Wrapping the
// render in `act` and awaiting it once, here, keeps every existing assertion
// below synchronous rather than rewriting 19 tests to `find*`.
async function renderSignIn() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const rootRoute = createRootRoute({ component: () => <SignInScreen /> });
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  let result!: ReturnType<typeof render>;
  await act(async () => {
    result = render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
  });
  return result;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SignInScreen", () => {
  it("renders the design's welcome copy", async () => {
    await renderSignIn();

    expect(screen.getByText("Welcome back.")).toBeInTheDocument();
    expect(
      screen.getByText("Sign in to pick up where you left off."),
    ).toBeInTheDocument();
  });

  // Task 30: the design's footer link into sign-up -- the only entry point
  // into the create-household flow from this screen.
  it("links to /sign-up from the 'No household yet?' footer", async () => {
    await renderSignIn();

    expect(screen.getByText("No household yet?")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create one" })).toHaveAttribute(
      "href",
      "/sign-up",
    );
  });

  it("submits valid credentials to POST /api/v1/auth/sign-in exactly once", async () => {
    // Only /auth/sign-in is registered -- if "Continue" ever posted anywhere
    // else (e.g. the magic-link endpoint), stubFetchRoutes would throw
    // rather than let this test pass on the wrong request.
    const fetchMock = stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: { status: 200, body: meBody },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(SIGN_IN_URL);
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({
      email: "andreas@hearth.family",
      password: "a-strong-password",
    });
  });

  it("renders the exact two-tries-left copy on a 401 with attemptsRemaining: 2", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 401,
        body: {
          error: {
            code: "INVALID_CREDENTIALS",
            message: "That email or password is incorrect.",
            details: { attemptsRemaining: 2 },
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(
      await screen.findByText(
        "That password doesn't match. Two tries left before we lock the household for 15 minutes.",
      ),
    ).toBeInTheDocument();
  });

  it("pluralises attemptsRemaining: 1 as 'One try left', not '1 tries left'", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 401,
        body: {
          error: {
            code: "INVALID_CREDENTIALS",
            message: "That email or password is incorrect.",
            details: { attemptsRemaining: 1 },
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(
      await screen.findByText(
        "That password doesn't match. One try left before we lock the household for 15 minutes.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/1 tries left/)).not.toBeInTheDocument();
  });

  it("renders the locked message and disables the submit button on a 423", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 423,
        body: {
          error: {
            code: "HOUSEHOLD_LOCKED",
            message:
              "This household is temporarily locked after too many failed sign-in attempts.",
            details: { lockedUntil: "2026-07-26T13:00:00Z" },
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    // Wait for the mutation to settle (the locked error to actually render)
    // before asserting on `disabled` -- otherwise this could pass merely by
    // catching the button's transient, unrelated isPending-disabled state
    // while the request is still in flight.
    await screen.findByText(
      "This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.",
    );
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    ).toBeEnabled();
  });

  // Fix round 4, Finding 2: signInError (and the locked state it drives)
  // used to clear only inside handleSubmit, which cannot run while Continue
  // is disabled -- so once the household locked there was no way back to a
  // usable form short of reloading the page. Editing either field must
  // re-enable it.
  it("re-enables Continue when the user edits the password after being locked", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 423,
        body: {
          error: {
            code: "HOUSEHOLD_LOCKED",
            message:
              "This household is temporarily locked after too many failed sign-in attempts.",
            details: { lockedUntil: "2026-07-26T13:00:00Z" },
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText(
      "This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.",
    );
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-different-password" },
    });

    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("re-enables Continue when the user edits the email after being locked", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 423,
        body: {
          error: {
            code: "HOUSEHOLD_LOCKED",
            message:
              "This household is temporarily locked after too many failed sign-in attempts.",
            details: { lockedUntil: "2026-07-26T13:00:00Z" },
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText(
      "This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.",
    );
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "christine@hearth.family" },
    });

    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  // Fix round 4, Finding 3: "Forgot?" had no `disabled` while its sibling
  // ("Email me a one-time sign-in link") did -- the same
  // guard-on-one-control-not-its-sibling class this project hit repeatedly.
  it("disables Forgot? while a magic-link request is pending, matching its sibling", async () => {
    let resolveSend!: () => void;
    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >((input, init) => {
      const url = String(input);
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "POST" && url === MAGIC_LINK_URL) {
        return new Promise<Response>((resolve) => {
          resolveSend = () =>
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
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Forgot?" }),
      ).toBeDisabled(),
    );
    expect(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    ).toBeDisabled();

    await act(async () => {
      resolveSend();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  });

  // Fix round 4, Finding 3: neither magic-link control is inside the <form>
  // (both are type="button"), so neither honours the email field's
  // `required` attribute -- clicking with an empty field used to post
  // straight to the API.
  it("shows an inline message and makes no request when Forgot? is clicked with an empty email", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.click(screen.getByRole("button", { name: "Forgot?" }));

    expect(
      screen.getByText("Enter your email address to get a sign-in link."),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows an inline message and makes no request when Email me... is clicked with an empty email", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    expect(
      screen.getByText("Enter your email address to get a sign-in link."),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows an inline message when the email does not look like an email address", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "not-an-email" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Forgot?" }));

    expect(
      screen.getByText("Enter your email address to get a sign-in link."),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // Fix round 1, Finding 5: clicking the magic-link control while locked
  // must not erase the lock explanation before the new request resolves --
  // the locked state and the magic-link request's own error are tracked
  // separately for exactly this reason.
  it("keeps the locked message on screen when a magic-link click fails", async () => {
    stubFetchRoutes({
      [`POST ${SIGN_IN_URL}`]: {
        status: 423,
        body: {
          error: {
            code: "HOUSEHOLD_LOCKED",
            message:
              "This household is temporarily locked after too many failed sign-in attempts.",
            details: { lockedUntil: "2026-07-26T13:00:00Z" },
          },
        },
      },
      [`POST ${MAGIC_LINK_URL}`]: {
        status: 429,
        body: { error: { code: "RATE_LIMITED", message: "Too many requests. Try again later." } },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const lockedMessage =
      "This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.";
    await screen.findByText(lockedMessage);

    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    expect(
      await screen.findByText("Too many requests. Try again later."),
    ).toBeInTheDocument();
    // The locked explanation must still be there -- not replaced, not gone.
    expect(screen.getByText(lockedMessage)).toBeInTheDocument();
  });

  it("requests a magic link and shows the sent confirmation instead of the form", async () => {
    const fetchMock = stubFetchRoutes({
      [`POST ${MAGIC_LINK_URL}`]: { status: 202, body: { status: "accepted" } },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(MAGIC_LINK_URL);
    expect(JSON.parse(init?.body as string)).toEqual({
      email: "andreas@hearth.family",
    });

    expect(await screen.findByText("Check your email.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Continue" }),
    ).not.toBeInTheDocument();
  });

  it("has a password field with type=password and autoComplete=current-password", async () => {
    await renderSignIn();

    const passwordField = screen.getByLabelText("Password");
    expect(passwordField).toHaveAttribute("type", "password");
    expect(passwordField).toHaveAttribute("autoComplete", "current-password");
  });

  // Fix round 1, Finding 1: a failed magic-link request must not look like a
  // no-op. Magic link is the only way back in while a household is locked,
  // so an offer that can fail silently is not a real offer.
  it("shows an error when the magic-link request itself fails (e.g. rate-limited)", async () => {
    stubFetchRoutes({
      [`POST ${MAGIC_LINK_URL}`]: {
        status: 429,
        body: { error: { code: "RATE_LIMITED", message: "Too many requests. Try again later." } },
      },
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    expect(
      await screen.findByText("Too many requests. Try again later."),
    ).toBeInTheDocument();
    // Must still be the password form, not the sent-confirmation panel.
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
  });

  // Fix round 1, Finding 1 (resend path): the sent panel's "Send another
  // link" calls the same handler as the original send, so a failed resend
  // must surface its own error rather than looking identical to success.
  // The magic-link route is given two responses in order: 202 for the
  // original send, then 500 for the resend.
  it("shows an error in the sent panel when a resend fails", async () => {
    stubFetchRoutes({
      [`POST ${MAGIC_LINK_URL}`]: [
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
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );
    await screen.findByText("Check your email.");

    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));

    expect(
      await screen.findByText(
        "Something went wrong. Please try again, or quote this reference if it keeps happening.",
      ),
    ).toBeInTheDocument();
  });

  // Fix round 1, Finding 2: sign-in's onError must not discard a non-ApiError
  // rejection (a network failure, a schema-parse error) -- it must still
  // show something rather than silently re-enabling the form.
  it("falls back to a generic message when sign-in rejects with something that isn't an ApiError", async () => {
    const fetchMock = vi.fn(() => Promise.reject(new TypeError("Failed to fetch")));
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(
      await screen.findByText("Something went wrong. Please try again."),
    ).toBeInTheDocument();
  });

  // Fix round 2, Finding 1: the resend-failure message must not survive a
  // return to password mode -- it belongs to the sent panel, not the form.
  it("clears a stale resend-failure message when returning to the password form", async () => {
    stubFetchRoutes({
      [`POST ${MAGIC_LINK_URL}`]: [
        { status: 202, body: { status: "accepted" } },
        {
          status: 500,
          body: { error: { code: "INTERNAL", message: "Something went wrong." } },
        },
      ],
    });
    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );
    await screen.findByText("Check your email.");

    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));
    await screen.findByText("Something went wrong.");

    fireEvent.click(screen.getByRole("button", { name: "Use a password instead" }));

    // Back on the password form, with no fresh request made yet -- the old
    // resend failure must not still be showing.
    expect(await screen.findByLabelText("Password")).toBeInTheDocument();
    expect(screen.queryByText("Something went wrong.")).not.toBeInTheDocument();
  });

  // Fix round 3, Finding 1: clearing magicLinkError on mode change (the fix
  // above) only clears an error that already exists at the moment the mode
  // changes. It does nothing for a request that is still in flight when the
  // user navigates away and only settles afterwards. "Send another link" has
  // no pending guard on "Use a password instead", so that interleaving is
  // reachable: start a resend, switch to password mode before it settles,
  // then let it reject -- onError must not render its result anywhere,
  // because by the time it fires the request no longer belongs to the
  // current mode.
  //
  // The resend's fetch is a deferred promise resolved manually, so the
  // interleaving is deterministic rather than timing-dependent -- a version
  // that raced real timers could pass by luck and would not pin the fix.
  it("does not render a stale resend error if 'Use a password instead' is clicked while the resend is still in flight", async () => {
    let rejectResend!: (reason: unknown) => void;
    let resendCallCount = 0;

    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >((input, init) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const url = String(input);
      if (method === "POST" && url === MAGIC_LINK_URL) {
        resendCallCount += 1;
        if (resendCallCount === 1) {
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

    await renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );
    await screen.findByText("Check your email.");

    // Start the resend -- it will not settle until rejectResend() is called.
    fireEvent.click(screen.getByRole("button", { name: "Send another link" }));
    await waitFor(() => expect(resendCallCount).toBe(2));

    // While it's still in flight, navigate back to the password form.
    fireEvent.click(
      screen.getByRole("button", { name: "Use a password instead" }),
    );
    await screen.findByLabelText("Password");

    // Now the abandoned resend fails. Flush a macrotask so every microtask
    // in the mutation's internal chain (fetch rejects -> onError runs) has
    // definitely completed before asserting.
    await act(async () => {
      rejectResend(new TypeError("Failed to fetch"));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // Still on the password form, and nothing rendered from the abandoned
    // request -- no alert region at all, since this test never attempted a
    // sign-in either.
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // Fix round 1, Item 1 (CONTROLLER RULING R13): the popup must be opened
  // synchronously, inside the click itself, not after the fetch resolves --
  // WebKit gates window.open on the synchronous gesture call stack, and an
  // awaited fetch reliably breaks that gate. The fetch is held open with a
  // manually-resolved promise specifically so this test can assert the open
  // call happened *before* anything about the request has settled, not just
  // that it eventually happened somewhere in the sequence.
  it("opens a blank tab synchronously in the click, then points it at the returned deep link once the request resolves", async () => {
    const fakePopup = { location: { href: "" }, close: vi.fn() };
    const open = vi.fn(() => fakePopup as unknown as Window);
    vi.stubGlobal("open", open);

    let resolveStart!: (response: Response) => void;
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url === TELEGRAM_START_URL) {
        return new Promise<Response>((resolve) => {
          resolveStart = resolve;
        });
      }
      throw new Error(`unexpected fetch call: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );

    // The fetch above is still pending (resolveStart hasn't been called
    // yet) -- so if this assertion holds, window.open ran inside the click
    // itself, not inside an onSuccess that waited on the network. No
    // "noopener" argument: that makes window.open return null per spec,
    // which would leave nothing to navigate below.
    expect(open).toHaveBeenCalledWith("", "_blank");
    expect(open).toHaveBeenCalledTimes(1);
    expect(fakePopup.location.href).toBe("");

    // TanStack Query dispatches the mutation's fetch asynchronously (a
    // microtask or two after mutate() is called), so `resolveStart` isn't
    // assigned the instant fireEvent.click returns -- wait for the request
    // to actually be in flight before resolving it. This waits on the
    // network call being *dispatched*, not on anything settling, so it
    // doesn't undercut the synchronous-open assertions above.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      resolveStart(
        new Response(
          JSON.stringify({
            url: "https://t.me/HearthBot?start=abc123",
            expiresAt: "2026-09-01T10:10:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // The same handle is pointed somewhere else -- not a second
    // window.open call once the URL is known.
    expect(fakePopup.location.href).toBe(
      "https://t.me/HearthBot?start=abc123",
    );
    expect(open).toHaveBeenCalledTimes(1);
  });

  // Fix round 1, Item 1: the reviewer specifically asked for this branch --
  // jsdom cannot model real user-activation gating, but it can return null
  // from a stubbed window.open, which is the exact shape a hard-blocked
  // popup takes. Silence here would be the bug the synchronous-open shape
  // exists to prevent, so a link must appear instead.
  it("surfaces the deep link as a tappable link when the synchronous window.open comes back null (popups blocked)", async () => {
    const open = vi.fn(() => null);
    vi.stubGlobal("open", open);
    stubFetchRoutes({
      [`POST ${TELEGRAM_START_URL}`]: {
        status: 200,
        body: {
          url: "https://t.me/HearthBot?start=abc123",
          expiresAt: "2026-09-01T10:10:00Z",
        },
      },
    });
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );

    const link = await screen.findByRole("link", { name: "Open Telegram" });
    expect(link).toHaveAttribute(
      "href",
      "https://t.me/HearthBot?start=abc123",
    );
  });

  // The control must not appear on an install with no bot configured, where
  // the endpoint answers 404 -- a button that always fails is worse than no
  // button.
  it("hides Continue with Telegram once the endpoint answers 404", async () => {
    // Stubbed even though this test doesn't assert on it: the click handler
    // now calls window.open synchronously on every click, regardless of the
    // eventual outcome, so a real jsdom window.open would otherwise run.
    vi.stubGlobal("open", vi.fn(() => ({ location: { href: "" }, close: vi.fn() })));
    stubFetchRoutes({
      [`POST ${TELEGRAM_START_URL}`]: {
        status: 404,
        body: {
          error: {
            code: "NOT_FOUND",
            message: "That endpoint does not exist.",
          },
        },
      },
    });
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );

    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Continue with Telegram" }),
      ).not.toBeInTheDocument(),
    );
  });

  // A non-404 failure (e.g. the bot API itself erroring) is a different case
  // from "no bot configured" -- the control stays, but the person sees
  // something rather than a click that silently did nothing. Rejects with a
  // network TypeError rather than a well-formed ApiError, matching "falls
  // back to a generic message when sign-in rejects with something that isn't
  // an ApiError" above: apiErrorMessage shows an ApiError's own server
  // message when there is one, and only falls back to TELEGRAM_FALLBACK_ERROR
  // when there isn't a safe message to show at all.
  it("shows the fallback message and keeps the control when the request itself fails (not an ApiError)", async () => {
    const popupClose = vi.fn();
    vi.stubGlobal(
      "open",
      vi.fn(() => ({ location: { href: "" }, close: popupClose })),
    );
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url === TELEGRAM_START_URL) {
        return Promise.reject(new TypeError("Failed to fetch"));
      }
      throw new Error(`unexpected fetch call: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );

    expect(
      await screen.findByText(
        "We could not start Telegram sign-in just now. Try again in a moment.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    ).toBeInTheDocument();
    // The blank tab opened synchronously on click has nowhere useful to go
    // once the request fails -- it must not be left dangling on about:blank.
    expect(popupClose).toHaveBeenCalledTimes(1);
  });

  // Fix round 1, Item 2 (CONTROLLER RULING R12): this file already carries
  // two earlier instances of the identical defect class --
  // magicLinkError/magicLinkValidationError not clearing across a state
  // transition ("Fix round 2, Finding 1" and "Fix round 3, Finding 1") --
  // so a stale telegramError surviving a successful retry would be the
  // third. The assertion right after the second click (before the retried
  // request even resolves) is deliberate: it pins that the banner is
  // cleared by the click itself, not merely as an incidental side effect of
  // the retry eventually succeeding.
  it("clears a stale Telegram error as soon as a later attempt is started, not just once it succeeds", async () => {
    const fakePopup = { location: { href: "" }, close: vi.fn() };
    vi.stubGlobal("open", vi.fn(() => fakePopup as unknown as Window));
    stubFetchRoutes({
      [`POST ${TELEGRAM_START_URL}`]: [
        {
          status: 500,
          body: {
            error: { code: "INTERNAL", message: "Something went wrong." },
          },
        },
        {
          status: 200,
          body: {
            url: "https://t.me/HearthBot?start=abc123",
            expiresAt: "2026-09-01T10:10:00Z",
          },
        },
      ],
    });
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );
    await screen.findByText("Something went wrong.");

    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Telegram" }),
    );

    // Cleared immediately by the second click's own handler -- before the
    // retried request has had any chance to resolve.
    expect(screen.queryByText("Something went wrong.")).not.toBeInTheDocument();

    await waitFor(() =>
      expect(fakePopup.location.href).toBe(
        "https://t.me/HearthBot?start=abc123",
      ),
    );
  });
});
