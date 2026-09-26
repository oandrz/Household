// Not part of the task-19 brief's enumerated Sidebar/Modal tests, but this
// screen is new, non-trivial behaviour (the route tree it belongs to,
// `/sign-in/magic`, is itself a Produces-list item) and has exactly one bug
// worth guarding against: the magic-link token is single-use, so firing the
// consume request twice (StrictMode double-invokes effects) would turn a
// successful sign-in into a visible failure on the second call.
import { StrictMode } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Me } from "./schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { MagicLinkConsumeScreen } from "./MagicLinkConsumeScreen";

function meFixture(): Me {
  return {
    user: {
      id: "user-1",
      email: "andreas@hearth.family",
      displayName: "Andreas",
      avatarInitial: "A",
    },
    household: {
      id: "household-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "membership-1",
      householdId: "household-1",
      userId: "user-1",
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

const signedOut = {
  "GET /api/v1/auth/me": {
    status: 401,
    body: { error: { code: "UNAUTHENTICATED", message: "Sign in to continue." } },
  },
};

function consumeCalls(fetchMock: ReturnType<typeof stubFetchRoutes>) {
  return fetchMock.mock.calls.filter(
    ([input]) => String(input) === "/api/v1/auth/magic-link/consume",
  );
}

describe("MagicLinkConsumeScreen", () => {
  // Login CSRF: anyone can mail themselves a magic link and send the URL to
  // someone else. If opening it signed the visitor in by itself, the victim
  // would land in the attacker's household and type their real balances
  // into it. Mail scanners that pre-open links would also spend the single-
  // use token before the person ever clicked. So opening the link must do
  // nothing until the person asks.
  it("does not consume the token until the person clicks to continue", async () => {
    const fetchMock = stubFetchRoutes({
      ...signedOut,
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    renderWithRouter(<MagicLinkConsumeScreen token="tok123" />);

    expect(
      await screen.findByRole("button", { name: /continue signing in/i }),
    ).toBeInTheDocument();
    // Let any effect that would fire on mount run before asserting silence.
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(consumeCalls(fetchMock)).toHaveLength(0);
  });

  it("consumes the token exactly once when the person clicks, even under StrictMode", async () => {
    const fetchMock = stubFetchRoutes({
      ...signedOut,
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    renderWithRouter(
      <StrictMode>
        <MagicLinkConsumeScreen token="tok123" />
      </StrictMode>,
    );

    const button = await screen.findByRole("button", { name: /continue signing in/i });
    fireEvent.click(button);
    fireEvent.click(button);

    await waitFor(() => expect(consumeCalls(fetchMock)).toHaveLength(1));
  });

  it("warns when someone is already signed in on this device", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    renderWithRouter(<MagicLinkConsumeScreen token="tok123" />);

    const warning = await screen.findByRole("status");
    expect(warning).toHaveTextContent(/signed in as andreas/i);
    expect(warning).toHaveTextContent(/sign them out/i);
  });

  it("shows the server's error message when the token is invalid or expired", async () => {
    stubFetchRoutes({
      ...signedOut,
      "POST /api/v1/auth/magic-link/consume": {
        status: 410,
        body: {
          error: { code: "TOKEN_EXPIRED", message: "This link has expired." },
        },
      },
    });

    renderWithRouter(<MagicLinkConsumeScreen token="tok123" />);
    fireEvent.click(await screen.findByRole("button", { name: /continue signing in/i }));

    expect(await screen.findByText("This link has expired.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /back to sign in/i })).toBeInTheDocument();
  });

  // Asserts the screen's correct, intended behaviour: a successful consume
  // navigates to "/". It documents the fix for a real bug found in real
  // Chromium during the identity slice's definition-of-done walkthrough
  // (the consume request succeeded and the session became valid, but the
  // screen never left "Signing you in…" -- see useConsumeMagicLink's
  // comment in useAuth.ts for the full mechanism), but it is not, on its
  // own, a regression guard for that bug: reverting the fix and re-running
  // this exact test (confirmed empirically) still passes, wrapped in
  // StrictMode or not -- jsdom does not reproduce the timing this bug
  // depended on, the same class of gap the plan's history records for Task
  // 19's Modal (jsdom's HTMLDialogElement has no showModal at all, so a
  // real-browser-only crash passed every test). mutationObserverCallbacks.
  // test.ts pins the underlying library asymmetry the fix depends on, but
  // that isn't a regression guard for this app-level bug either -- it
  // would stay green even if the anti-pattern crept back in here. The real
  // defence against that is the real-browser walkthrough. This test stays
  // for what it does prove: the happy path lands on "/". Asserting on
  // `router.state.location.pathname` rather than rendered content, because
  // this test harness's single-route tree renders identical content at
  // every path -- content alone can't distinguish "navigated" from "never
  // tried."
  it("navigates to / once the link is consumed successfully", async () => {
    stubFetchRoutes({
      ...signedOut,
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    const { router } = renderWithRouter(
      <StrictMode>
        <MagicLinkConsumeScreen token="tok123" />
      </StrictMode>,
      "/sign-in/magic",
    );
    fireEvent.click(await screen.findByRole("button", { name: /continue signing in/i }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });
});
