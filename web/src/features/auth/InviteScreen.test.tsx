import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { InviteScreen } from "./InviteScreen";

// Mirrors invite_handlers.go's invitePreviewResponse.
const previewBody = {
  householdName: "Oentoro household",
  inviterName: "Christine",
  name: "Andreas",
  role: "owner",
  capabilities: ["calendar", "chores", "money", "marriage"],
};

// Mirrors auth_handlers.go's meResponseBody -- what a successful
// /invites/:token/accept answers with (completeSignIn).
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

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// Each call to fetch consumes the next entry; the last entry repeats if
// fetch is called more times than provided for.
function stubFetchSequence(responses: Array<{ status: number; body: unknown }>) {
  let call = 0;
  const fetchMock = vi.fn<
    (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
  >(async () => {
    const next = responses[Math.min(call, responses.length - 1)];
    call += 1;
    return jsonResponse(next.status, next.body);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderInvite(token = "tok123") {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <InviteScreen token={token} />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("InviteScreen", () => {
  it("renders the inviter, household name and role from a successful preview", async () => {
    stubFetchSequence([{ status: 200, body: previewBody }]);
    renderInvite();

    expect(
      await screen.findByText("Christine invited you in."),
    ).toBeInTheDocument();
    expect(screen.getByText("Oentoro household")).toBeInTheDocument();
    expect(screen.getByText("co-owner")).toBeInTheDocument();
  });

  it("renders a not-found message on a 404 preview", async () => {
    stubFetchSequence([
      { status: 404, body: { error: { code: "NOT_FOUND", message: "That could not be found." } } },
    ]);
    renderInvite();

    expect(
      await screen.findByText(
        "We couldn't find that invite. Check the link, or ask whoever invited you to send a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("renders an expired message on a 410 preview", async () => {
    stubFetchSequence([
      { status: 410, body: { error: { code: "INVITE_EXPIRED", message: "This invite has expired." } } },
    ]);
    renderInvite();

    expect(
      await screen.findByText(
        "This invite has expired. Ask whoever invited you to send a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("renders an already-accepted message with a sign-in link on a 409 preview", async () => {
    stubFetchSequence([
      {
        status: 409,
        body: {
          error: {
            code: "INVITE_ALREADY_ACCEPTED",
            message: "This invite has already been accepted.",
          },
        },
      },
    ]);
    renderInvite();

    expect(
      await screen.findByText(/This invite has already been accepted\./),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/",
    );
  });

  it("surfaces the server's message on a 422 PASSWORD_TOO_SHORT", async () => {
    const fetchMock = stubFetchSequence([
      { status: 200, body: previewBody },
      {
        status: 422,
        body: {
          error: {
            code: "PASSWORD_TOO_SHORT",
            message: "Password must be at least 12 characters.",
          },
        },
      },
    ]);
    renderInvite();

    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "tooshort" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    expect(
      await screen.findByText("Password must be at least 12 characters."),
    ).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    // Pins the stub's positional assumption (call 1 = preview GET, call 2 =
    // accept POST) to something that fails loudly if a refetch or remount
    // ever shifts it, rather than silently asserting against the wrong call.
    expect(fetchMock.mock.calls[1][0]).toBe(
      "/api/v1/invites/tok123/accept",
    );
  });

  it("surfaces the server's message on a 422 PASSWORD_TOO_LONG", async () => {
    stubFetchSequence([
      { status: 200, body: previewBody },
      {
        status: 422,
        body: {
          error: {
            code: "PASSWORD_TOO_LONG",
            message: "Password must be at most 256 characters.",
          },
        },
      },
    ]);
    renderInvite();

    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "x".repeat(300) },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    expect(
      await screen.findByText("Password must be at most 256 characters."),
    ).toBeInTheDocument();
  });

  it("accepts the invite, calling the accept endpoint exactly once", async () => {
    const fetchMock = stubFetchSequence([
      { status: 200, body: previewBody },
      { status: 200, body: meBody },
    ]);
    renderInvite("tok123");

    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password-12" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const [url, init] = fetchMock.mock.calls[1];
    expect(url).toBe("/api/v1/invites/tok123/accept");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({
      password: "a-strong-password-12",
      displayName: "Andreas",
    });
  });
});
