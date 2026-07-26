import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SignInScreen } from "./SignInScreen";

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

function stubFetch(status: number, body: unknown) {
  const fetchMock = vi.fn<
    (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
  >(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderSignIn() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SignInScreen />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SignInScreen", () => {
  it("renders the design's welcome copy", () => {
    renderSignIn();

    expect(screen.getByText("Welcome back.")).toBeInTheDocument();
    expect(
      screen.getByText("Sign in to pick up where you both left off."),
    ).toBeInTheDocument();
  });

  it("submits valid credentials to POST /api/v1/auth/sign-in exactly once", async () => {
    const fetchMock = stubFetch(200, meBody);
    renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/auth/sign-in");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({
      email: "andreas@hearth.family",
      password: "a-strong-password",
    });
  });

  it("renders the exact two-tries-left copy on a 401 with attemptsRemaining: 2", async () => {
    stubFetch(401, {
      error: {
        code: "INVALID_CREDENTIALS",
        message: "That email or password is incorrect.",
        details: { attemptsRemaining: 2 },
      },
    });
    renderSignIn();

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
    stubFetch(401, {
      error: {
        code: "INVALID_CREDENTIALS",
        message: "That email or password is incorrect.",
        details: { attemptsRemaining: 1 },
      },
    });
    renderSignIn();

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
    stubFetch(423, {
      error: {
        code: "HOUSEHOLD_LOCKED",
        message:
          "This household is temporarily locked after too many failed sign-in attempts.",
        details: { lockedUntil: "2026-07-26T13:00:00Z" },
      },
    });
    renderSignIn();

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

  // Fix round 1, Finding 5: clicking the magic-link control while locked
  // must not erase the lock explanation before the new request resolves --
  // the locked state and the magic-link request's own error are tracked
  // separately for exactly this reason.
  it("keeps the locked message on screen when a magic-link click fails", async () => {
    let call = 0;
    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >(async () => {
      call += 1;
      if (call === 1) {
        // The sign-in attempt that gets the household locked.
        return new Response(
          JSON.stringify({
            error: {
              code: "HOUSEHOLD_LOCKED",
              message:
                "This household is temporarily locked after too many failed sign-in attempts.",
              details: { lockedUntil: "2026-07-26T13:00:00Z" },
            },
          }),
          { status: 423, headers: { "Content-Type": "application/json" } },
        );
      }
      // The magic-link click that follows, which fails.
      return new Response(
        JSON.stringify({
          error: { code: "RATE_LIMITED", message: "Too many requests. Try again later." },
        }),
        { status: 429, headers: { "Content-Type": "application/json" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);
    renderSignIn();

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
    const fetchMock = stubFetch(202, { status: "accepted" });
    renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/auth/magic-link");
    expect(JSON.parse(init?.body as string)).toEqual({
      email: "andreas@hearth.family",
    });

    expect(await screen.findByText("Check your email.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Continue" }),
    ).not.toBeInTheDocument();
  });

  it("has a password field with type=password and autoComplete=current-password", () => {
    renderSignIn();

    const passwordField = screen.getByLabelText("Password");
    expect(passwordField).toHaveAttribute("type", "password");
    expect(passwordField).toHaveAttribute("autoComplete", "current-password");
  });

  // Fix round 1, Finding 1: a failed magic-link request must not look like a
  // no-op. Magic link is the only way back in while a household is locked,
  // so an offer that can fail silently is not a real offer.
  it("shows an error when the magic-link request itself fails (e.g. rate-limited)", async () => {
    stubFetch(429, {
      error: { code: "RATE_LIMITED", message: "Too many requests. Try again later." },
    });
    renderSignIn();

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
  it("shows an error in the sent panel when a resend fails", async () => {
    stubFetch(202, { status: "accepted" });
    renderSignIn();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "andreas@hearth.family" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a one-time sign-in link" }),
    );
    await screen.findByText("Check your email.");

    stubFetch(500, { error: { code: "INTERNAL", message: "Something went wrong. Please try again, or quote this reference if it keeps happening." } });
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
    renderSignIn();

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
});
