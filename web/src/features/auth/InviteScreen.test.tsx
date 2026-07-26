import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, fireEvent } from "@testing-library/react";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { InviteScreen } from "./InviteScreen";

const PREVIEW_URL = "/api/v1/invites/tok123";
const ACCEPT_URL = "/api/v1/invites/tok123/accept";
const ME_URL = "/api/v1/auth/me";

// InviteScreen now calls useMe() too (fix round 1, Finding 3 -- it warns
// when a live session already exists), so every test needs a registered
// response for it, not just the ones that care about the warning. Without
// this, stubFetchRoutes would throw "no stub registered" inside apiFetch,
// which useQuery's queryFn swallows into an ordinary error state --
// coincidentally identical, from useMe()'s point of view, to a real 401.
// That coincidence is exactly the kind of accidental pass this stub exists
// to prevent (see test/fetchStub.ts's own header comment), so this is
// registered explicitly rather than left to fall through.
const NO_SESSION = {
  status: 401,
  body: { error: { code: "UNAUTHENTICATED", message: "Sign in required." } },
};

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

// A live session for someone other than tok123's invitee (Andreas) -- e.g. a
// shared family device Christine is still signed into when Andreas opens his
// own invite link on it.
const signedInAsSomeoneElse = {
  user: {
    id: "u-christine",
    email: "christine@hearth.family",
    displayName: "Christine",
    avatarInitial: "C",
  },
  household: {
    id: "h-oentoro",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: false,
    secondaryCurrency: "",
    fxRateMode: "static",
  },
  membership: {
    id: "m-christine",
    householdId: "h-oentoro",
    userId: "u-christine",
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
    // The preview GET and the /auth/me check are the only two registered --
    // if InviteScreen ever fetched a different URL for this render,
    // stubFetchRoutes would throw "no stub registered" and the test would
    // fail loudly instead of quietly passing on a canned response meant for
    // a different request.
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`GET ${ME_URL}`]: NO_SESSION,
    });
    renderInvite();

    expect(
      await screen.findByText("Christine invited you in."),
    ).toBeInTheDocument();
    expect(screen.getByText("Oentoro household")).toBeInTheDocument();
    expect(screen.getByText("co-owner")).toBeInTheDocument();
  });

  it("warns when a live session exists for someone else, without blocking acceptance", async () => {
    // Fix round 1, Finding 3: the invite route stays public on purpose (a
    // shared device might already be signed in as a different household
    // member) -- but accepting silently overwrites that session with no
    // warning. This asserts the warning names the currently-signed-in
    // person and states what accepting will do, and that it does not
    // prevent the form from being used.
    const fetchMock = stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`GET ${ME_URL}`]: { status: 200, body: signedInAsSomeoneElse },
      [`POST ${ACCEPT_URL}`]: { status: 200, body: meBody },
    });
    renderInvite();

    const warning = await screen.findByText(
      /Accepting this invite will sign them out and sign you in instead\./,
    );
    expect(warning).toBeInTheDocument();
    expect(screen.getByText("Christine")).toBeInTheDocument();

    // Not blocking: the form is still usable underneath the warning.
    await screen.findByText("Christine invited you in.");
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-strong-password-12" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Accept & join household" }),
    );

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.filter(([input]) => String(input) === ACCEPT_URL),
      ).toHaveLength(1),
    );
  });

  it("does not warn when there is no live session", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: { status: 200, body: previewBody },
      [`GET ${ME_URL}`]: NO_SESSION,
    });
    renderInvite();

    await screen.findByText("Christine invited you in.");
    expect(
      screen.queryByText(/Accepting this invite will sign them out/),
    ).not.toBeInTheDocument();
  });

  it("renders a not-found message on a 404 preview", async () => {
    stubFetchRoutes({
      [`GET ${PREVIEW_URL}`]: {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That could not be found." } },
      },
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
      [`GET ${ME_URL}`]: NO_SESSION,
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
