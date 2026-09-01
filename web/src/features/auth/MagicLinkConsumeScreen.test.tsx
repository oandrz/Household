// Not part of the task-19 brief's enumerated Sidebar/Modal tests, but this
// screen is new, non-trivial behaviour (the route tree it belongs to,
// `/sign-in/magic`, is itself a Produces-list item) and has exactly one bug
// worth guarding against: the magic-link token is single-use, so firing the
// consume request twice (StrictMode double-invokes effects) would turn a
// successful sign-in into a visible failure on the second call.
import { StrictMode } from "react";
import { screen, waitFor } from "@testing-library/react";
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

describe("MagicLinkConsumeScreen", () => {
  it("consumes the token exactly once even under StrictMode's double-invoke", async () => {
    const fetchMock = stubFetchRoutes({
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    renderWithRouter(
      <StrictMode>
        <MagicLinkConsumeScreen token="tok123" />
      </StrictMode>,
    );

    await waitFor(() => {
      const calls = fetchMock.mock.calls.filter(
        ([input]) => String(input) === "/api/v1/auth/magic-link/consume",
      );
      expect(calls).toHaveLength(1);
    });
  });

  it("shows the server's error message when the token is invalid or expired", async () => {
    stubFetchRoutes({
      "POST /api/v1/auth/magic-link/consume": {
        status: 410,
        body: {
          error: { code: "TOKEN_EXPIRED", message: "This link has expired." },
        },
      },
    });

    renderWithRouter(<MagicLinkConsumeScreen token="tok123" />);

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
      "POST /api/v1/auth/magic-link/consume": { status: 200, body: meFixture() },
    });

    const { router } = renderWithRouter(
      <StrictMode>
        <MagicLinkConsumeScreen token="tok123" />
      </StrictMode>,
      "/sign-in/magic",
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });
});
