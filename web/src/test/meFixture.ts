// The signed-in bundle (GET /auth/me) for tests whose component reads the
// household's time zone. The zone is the argument because it is the one field
// those tests are about: every date the app shows comes from it
// (lib/householdDate.ts).
import type { Me } from "../features/auth/schemas";
import type { RouteResponse } from "./fetchStub";

function meFixture(timezone: string): Me {
  return {
    user: { id: "u1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "SGD",
      fxRateMode: "auto",
      timezone,
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

// The route entry to spread into stubFetchRoutes.
export function meRoute(timezone: string): Record<string, RouteResponse> {
  return { "GET /api/v1/auth/me": { status: 200, body: meFixture(timezone) } };
}
