import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, fireEvent } from "@testing-library/react";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { InviteScreen } from "./InviteScreen";

const PREVIEW_URL = "/api/v1/invites/tok123";
const ACCEPT_URL = "/api/v1/invites/tok123/accept";

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

// InviteScreen now calls useNavigate() (it navigates to "/" once acceptance
// succeeds -- see routes/router.tsx's comment on why the invite route itself
// does not redirect an already-signed-in visitor away), which throws outside
// a router context, so this needs renderWithRouter rather than a bare
// QueryClientProvider.
function renderInvite(token = "tok123") {
  return renderWithRouter(<InviteScreen token={token} />, `/invite/${token}`);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("InviteScreen", () => {
  it("renders the inviter, household name and role from a successful preview", async () => {
    // Only the preview GET is registered -- if InviteScreen ever fetched a
    // different URL for this render, stubFetchRoutes would throw "no stub
    // registered" and the test would fail loudly instead of quietly passing
    // on a canned response meant for a different request.
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
    });
    renderInvite();

    expect(
      await screen.findByText("Christine invited you in."),
    ).toBeInTheDocument();
    expect(screen.getByText("Oentoro household")).toBeInTheDocument();
    expect(screen.getByText("co-owner")).toBeInTheDocument();
  });

  it("renders a not-found message on a 404 preview", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That could not be found." } },
      },
    });
    renderInvite();

    expect(
      await screen.findByText(
        "We couldn't find that invite. Check the link, or ask whoever invited you to send a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("renders an expired message on a 410 preview", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: {
        status: 410,
        body: { error: { code: "INVITE_EXPIRED", message: "This invite has expired." } },
      },
    });
    renderInvite();

    expect(
      await screen.findByText(
        "This invite has expired. Ask whoever invited you to send a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("renders an already-accepted message with a sign-in link on a 409 preview", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: {
        status: 409,
        body: {
          error: {
            code: "INVITE_ALREADY_ACCEPTED",
            message: "This invite has already been accepted.",
          },
        },
      },
    });
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
    const fetchMock = stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`POST ${ACCEPT_URL}`]: {
        status: 422,
        body: {
          error: {
            code: "PASSWORD_TOO_SHORT",
            message: "Password must be at least 12 characters.",
          },
        },
      },
    });
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
    // Both counts matter: the accept call happened exactly once (the point
    // of the test), and the preview was not silently refetched in the
    // process (fix round 3 -- a filtered-length check on the accept call
    // alone would pass even if the preview GET fired twice).
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.filter(([input]) => String(input) === ACCEPT_URL),
      ).toHaveLength(1),
    );
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === PREVIEW_URL),
    ).toHaveLength(1);
  });

  it("surfaces the server's message on a 422 PASSWORD_TOO_LONG", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`POST ${ACCEPT_URL}`]: {
        status: 422,
        body: {
          error: {
            code: "PASSWORD_TOO_LONG",
            message: "Password must be at most 256 characters.",
          },
        },
      },
    });
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
    const fetchMock = stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`POST ${ACCEPT_URL}`]: { status: 200, body: meBody },
    });
    renderInvite("tok123");

    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password-12" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    // Both counts matter: the accept call happened exactly once, and the
    // preview was not fetched more than once either (fix round 3 -- the
    // filtered-length check on the accept call alone would pass even if the
    // preview GET fired twice, which the plain toHaveBeenCalledTimes(2) this
    // replaced would have caught).
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.filter(([input]) => String(input) === ACCEPT_URL),
      ).toHaveLength(1),
    );
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === PREVIEW_URL),
    ).toHaveLength(1);
    const acceptCall = fetchMock.mock.calls.find(
      ([input]) => String(input) === ACCEPT_URL,
    );
    const init = acceptCall?.[1];
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({
      password: "a-strong-password-12",
      displayName: "Andreas",
    });
  });

  it("navigates to / once acceptance succeeds", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`POST ${ACCEPT_URL}`]: { status: 200, body: meBody },
    });
    const { router } = renderInvite("tok123");
    expect(router.state.location.pathname).toBe("/invite/tok123");

    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password-12" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/"),
    );
  });
});
