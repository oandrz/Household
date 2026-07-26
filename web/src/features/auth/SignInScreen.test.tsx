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
});
