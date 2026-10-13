// The rule these tests pin: when the person signed in on this tab changes,
// nothing the previous person loaded may stay in the query cache.
//
// Query keys are not per-user (["accounts"], ["transactions"]), and a cached
// answer under 30 seconds old is shown without asking the server again. So a
// cache that outlives a change of person would show an owner's balances to
// the limited member who signs in next on a shared tablet, whatever the
// server would have sent them.
//
// Each test plants another person's data in the cache as an inactive query
// (loaded earlier, no screen showing it now), runs one of the five actions
// that change who is signed in, and asserts the data is gone.
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import type { Me } from "./schemas";
import {
  meQueryKey,
  useAcceptInvite,
  useCompleteSignUp,
  useConsumeMagicLink,
  useSignIn,
  useSignOut,
} from "./useAuth";

function me(overrides: { userId: string; displayName: string; role: "owner" | "limited" }): Me {
  const capabilities =
    overrides.role === "owner"
      ? ["calendar", "chores", "money", "marriage"]
      : ["calendar", "chores", "money"];
  return {
    user: {
      id: overrides.userId,
      email: `${overrides.displayName.toLowerCase()}@hearth.family`,
      displayName: overrides.displayName,
      avatarInitial: overrides.displayName[0],
    },
    household: {
      id: "household-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
      timezone: "Asia/Singapore",
    },
    membership: {
      id: `membership-${overrides.userId}`,
      householdId: "household-1",
      userId: overrides.userId,
      role: overrides.role,
      capabilities,
    },
    capabilities,
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

const OWNER = me({ userId: "user-owner", displayName: "Andreas", role: "owner" });
const NEXT_PERSON = me({ userId: "user-kayla", displayName: "Kayla", role: "limited" });

// What the owner's Finances screen left behind: both accounts, with balances.
const OWNERS_ACCOUNTS = {
  accounts: [
    { id: "a1", nickname: "Shared family savings", balance: { amountMinor: 4_690_000, currency: "SGD" } },
    { id: "a2", nickname: "Private owner account", balance: { amountMinor: 824_055, currency: "SGD" } },
  ],
};

type Hooks = {
  signIn: ReturnType<typeof useSignIn>;
  signOut: ReturnType<typeof useSignOut>;
  consume: ReturnType<typeof useConsumeMagicLink>;
  accept: ReturnType<typeof useAcceptInvite>;
  complete: ReturnType<typeof useCompleteSignUp>;
};

// Harness mounts all five hooks and runs whichever one the test asks for. It
// calls no useQuery of its own, so every query in the cache is inactive --
// exactly the state a previous person's data is in once their screens have
// unmounted.
function Harness({ run }: { run: (hooks: Hooks) => void }) {
  const hooks: Hooks = {
    signIn: useSignIn(),
    signOut: useSignOut(),
    consume: useConsumeMagicLink(),
    accept: useAcceptInvite(),
    complete: useCompleteSignUp(),
  };
  return (
    <button type="button" onClick={() => run(hooks)}>
      run
    </button>
  );
}

async function renderWithThePreviousPersonsCache(run: (hooks: Hooks) => void) {
  let queryClient!: QueryClient;
  await act(async () => {
    ({ queryClient } = renderWithRouter(<Harness run={run} />));
  });
  queryClient.setQueryData(meQueryKey, OWNER);
  queryClient.setQueryData(["accounts"], OWNERS_ACCOUNTS);
  queryClient.setQueryData(["transactions", { month: "2026-10" }], { transactions: [{ id: "t1" }] });
  return queryClient;
}

function cachedKeys(queryClient: QueryClient): string[] {
  return queryClient
    .getQueryCache()
    .getAll()
    .map((query) => JSON.stringify(query.queryKey))
    .sort();
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("a change of signed-in person empties the query cache", () => {
  it("sign-out leaves nothing behind, not even who was signed in", async () => {
    stubFetchRoutes({ "POST /api/v1/auth/sign-out": { status: 204, body: undefined } });
    const queryClient = await renderWithThePreviousPersonsCache((h) => h.signOut.mutate());

    fireEvent.click(screen.getByRole("button", { name: "run" }));

    await waitFor(() => expect(queryClient.getQueryData(["accounts"])).toBeUndefined());
    expect(cachedKeys(queryClient)).toEqual([]);
  });

  const signInActions: Array<{
    name: string;
    route: string;
    run: (hooks: Hooks) => void;
  }> = [
    {
      name: "sign-in",
      route: "POST /api/v1/auth/sign-in",
      run: (h) => h.signIn.mutate({ email: "kayla@hearth.family", password: "a long enough password" }),
    },
    {
      name: "magic-link sign-in",
      route: "POST /api/v1/auth/magic-link/consume",
      run: (h) => h.consume.mutate({ token: "tok" }),
    },
    {
      name: "accepting an invite",
      route: "POST /api/v1/invites/tok/accept",
      run: (h) =>
        h.accept.mutate({ token: "tok", password: "a long enough password", displayName: "Kayla" }),
    },
    {
      name: "finishing sign-up",
      route: "POST /api/v1/auth/sign-up/tok/complete",
      run: (h) =>
        h.complete.mutate({
          token: "tok",
          householdName: "Kayla's household",
          displayName: "Kayla",
          primaryCurrency: "SGD",
          timezone: "Asia/Singapore",
          password: "a long enough password",
        }),
    },
  ];

  it.each(signInActions)(
    "$name drops the previous person's data and stores the new person",
    async ({ route, run }) => {
      stubFetchRoutes({ [route]: { status: 200, body: NEXT_PERSON } });
      const queryClient = await renderWithThePreviousPersonsCache(run);

      fireEvent.click(screen.getByRole("button", { name: "run" }));

      await waitFor(() =>
        expect(queryClient.getQueryData<Me>(meQueryKey)?.user.id).toBe("user-kayla"),
      );
      expect(queryClient.getQueryData(["accounts"])).toBeUndefined();
      // Only the new person is left: no account list, no transactions.
      expect(cachedKeys(queryClient)).toEqual([JSON.stringify(meQueryKey)]);
    },
  );
});
