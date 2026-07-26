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
});
