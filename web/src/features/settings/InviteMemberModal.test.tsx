// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the role-driven capability forcing (an owner invite must
// carry every capability -- domain.ErrOwnerMustHoldAllCapabilities) and the
// "marriage not offered to a Kid" rule are non-trivial and untested
// otherwise. Task 12 adds the channel-picking behaviour: which channel gets
// sent, the three flag states, and the hand-off into the waiting card.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import type { Me } from "../auth/schemas";
import { meQueryKey } from "../auth/useAuth";
import { InviteMemberModal } from "./InviteMemberModal";
import type { RoleOption } from "./useInviteMember";

const INVITE_URL = "/api/v1/household/members/invite";
const INVITES_URL = "/api/v1/household/invites";
const ME_URL = "/api/v1/auth/me";

// Both channels on -- the baseline for tests that are about capability
// toggling or role switching, not about channel availability, so they don't
// accidentally land on the single-channel or unavailable branch.
const BOTH_CHANNELS = { email_invites: true, telegram_sign_in: true };

function meBody(features: Record<string, boolean>): Me {
  return {
    user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h-1", name: "Andreas & Christine", familyName: "Oentoro",
      primaryCurrency: "SGD", showSecondaryCurrency: false, secondaryCurrency: "", fxRateMode: "auto",
    },
    membership: { id: "mem-1", householdId: "h-1", userId: "u-1", role: "owner", capabilities: ["money"] },
    capabilities: ["money"],
    spaces: [],
    isPlatformAdmin: false,
    features,
  };
}

// `defaultRole` is required, not defaulted here, on purpose: the component's
// own default ("limited") only matches some of these tests, and a caller
// that means "owner" should say so rather than lean on a helper default a
// reader would have to go look up.
function renderModal({
  features,
  defaultRole,
  routes = {},
}: {
  features: Record<string, boolean>;
  defaultRole: RoleOption;
  routes?: Record<string, RouteResponse | RouteResponse[]>;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // Seeded before render, not just registered as a route below: useFeature
  // reads useMe().data synchronously on the first render, and
  // InviteMemberModal's own availableChannelsFor needs the real flags on
  // that very first render -- a fetch that only resolves a tick later would
  // leave the initial channel choice computed from two false flags.
  queryClient.setQueryData(meQueryKey, meBody(features));
  const stub = stubFetchRoutes({
    // Registered anyway, for the background refetch the default
    // `staleTime: 0` triggers on mount, and the one useInviteMember's own
    // onSuccess kicks off by invalidating meQueryKey.
    [`GET ${ME_URL}`]: { status: 200, body: meBody(features) },
    ...routes,
  });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <InviteMemberModal open defaultRole={defaultRole} onClose={() => {}} />
    </QueryClientProvider>,
  );
  return { ...utils, stub };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("InviteMemberModal", () => {
  it("does not offer the Marriage space capability for the default Kid role", () => {
    renderModal({ features: BOTH_CHANNELS, defaultRole: "limited" });
    expect(screen.queryByText("Marriage space")).not.toBeInTheDocument();
  });

  it("shows Marriage space, forced on and disabled, once Parent is selected", () => {
    renderModal({ features: BOTH_CHANNELS, defaultRole: "limited" });
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "owner" } });

    expect(screen.getByText("Marriage space")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Marriage space access" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Marriage space access" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    // domain.ErrOwnerMustHoldAllCapabilities -- an owner invite must force
    // every other capability on too, not just marriage.
    expect(screen.getByRole("switch", { name: "Calendar access" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Money & balances access" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("starts on Parent, with every capability forced on, when its opener asks for an owner", () => {
    renderModal({ features: BOTH_CHANNELS, defaultRole: "owner" });

    expect(screen.getByLabelText("Role")).toHaveValue("owner");
    expect(screen.getByRole("switch", { name: "Marriage space access" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("shows the design's 'off for kids by default' helper text on the money row", () => {
    renderModal({ features: BOTH_CHANNELS, defaultRole: "limited" });
    expect(screen.getByText("Off for kids by default")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Money & balances access" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("submits POST /api/v1/household/members/invite with the kid's default capabilities and channel: profile", async () => {
    // fetchStub.ts's own capture hook, not fetchMock.mock.calls -- this is
    // the file's stated reason for having one: reading the posted body from
    // the same map that registered the response, rather than reaching into
    // the mock's call history.
    let capturedBody: unknown;
    renderModal({
      features: { telegram_sign_in: true },
      defaultRole: "limited",
      routes: {
        [`POST ${INVITE_URL}`]: {
          status: 201,
          body: {},
          capture: (body) => {
            capturedBody = body;
          },
        },
      },
    });

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Kayla" } });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));

    await waitFor(() =>
      expect(capturedBody).toEqual({
        name: "Kayla",
        email: "",
        role: "limited",
        capabilities: ["calendar", "chores"],
        channel: "profile",
      }),
    );
  });

  it("submits every capability, by email, when the role is Parent and email is the only channel", async () => {
    let capturedBody: unknown;
    // email_invites on, telegram_sign_in off: email is the only owner
    // channel, so it is auto-selected with no radio group to click through
    // -- this test types straight into the address field, matching an
    // install with mail but no bot configured.
    renderModal({
      features: { email_invites: true, telegram_sign_in: false },
      defaultRole: "limited",
      routes: {
        [`POST ${INVITE_URL}`]: {
          status: 201,
          body: {},
          capture: (body) => {
            capturedBody = body;
          },
        },
      },
    });

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Christine" } });
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "owner" } });
    fireEvent.change(screen.getByLabelText(/email address/i), {
      target: { value: "christine@hearth.family" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));

    await waitFor(() =>
      expect(capturedBody).toEqual({
        name: "Christine",
        email: "christine@hearth.family",
        role: "owner",
        capabilities: ["calendar", "chores", "money", "marriage"],
        channel: "email",
      }),
    );
  });

  // With mail hidden, an owner invite is a Telegram link and needs no
  // address at all -- ErrInviteRequiresEmail guarded delivery, not identity
  // (spec decision 9).
  it("asks for no email when email invites are off", () => {
    renderModal({
      features: { email_invites: false, telegram_sign_in: true },
      defaultRole: "owner",
    });
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
  });

  it("offers the channel choice when the operator turns email invites on", () => {
    renderModal({
      features: { email_invites: true, telegram_sign_in: true },
      defaultRole: "owner",
    });
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  // Neither channel available: say so, rather than offering a button that
  // goes nowhere (spec decision 11). Only meaningful for the owner role --
  // "limited" always has "profile" (see InviteMemberModal's own
  // availableChannelsFor), which is exactly what the next test pins.
  it("says inviting is unavailable when neither channel exists", () => {
    renderModal({
      features: { email_invites: false, telegram_sign_in: false },
      defaultRole: "owner",
    });
    expect(screen.getByText(/inviting is unavailable on this install/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /send invite/i })).not.toBeInTheDocument();
  });

  // The modal does not close on success: the link is shown once, and closing
  // would throw it away.
  it("becomes the waiting card when a Telegram invite is created", async () => {
    renderModal({
      features: { email_invites: false, telegram_sign_in: true },
      defaultRole: "owner",
      routes: {
        [`POST ${INVITE_URL}`]: {
          status: 201,
          body: { id: "1", expiresAt: "2026-09-21T00:00:00Z", link: "https://t.me/HearthBot?start=inv_abc" },
        },
        // The card's own usePendingInvites({enabled: true}) call, once
        // `created` is set -- empty, so the card falls back to the row this
        // modal synthesizes from what it just sent.
        [`GET ${INVITES_URL}`]: { status: 200, body: [] },
      },
    });
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Christine" } });
    fireEvent.click(screen.getByRole("button", { name: /send invite/i }));
    expect(await screen.findByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByText(/shown once/i)).toBeInTheDocument();
  });

  // A limited member with no sign-in is today's kid path, untouched: the
  // member is created directly, with no invite row, so there is no channel to
  // choose and no link to hand over.
  it("keeps the profile-only path for a kid", async () => {
    let capturedBody: unknown;
    renderModal({
      features: { email_invites: false, telegram_sign_in: true },
      defaultRole: "owner",
      routes: {
        [`POST ${INVITE_URL}`]: {
          status: 201,
          body: {},
          capture: (body) => {
            capturedBody = body;
          },
        },
      },
    });
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Kayla" } });
    fireEvent.change(screen.getByLabelText(/role/i), { target: { value: "limited" } });
    fireEvent.click(screen.getByRole("button", { name: /send invite/i }));

    await waitFor(() => expect(capturedBody).toBeDefined());
    const body = capturedBody as { channel?: string; email?: string };
    // "profile" is said out loud: the route refuses an omitted channel, and a
    // kid writes no invite row, so it is not "email with no address".
    expect(body.channel).toBe("profile");
    expect(body.email ?? "").toBe("");
    // No link came back, so the modal closes rather than becoming a card.
    expect(screen.queryByRole("button", { name: /copy/i })).not.toBeInTheDocument();
  });

  // The round trip a previous review flagged as untested: prop `link`
  // present -> mint a new link -> onNewLink fires -> the modal's own
  // `created` state updates -> the card still shows the right link. This is
  // an integration smoke more than a proof of onNewLink specifically --
  // PendingInviteCard's own `heldLink` already displays whatever it mints
  // regardless of whether this modal listens, so deleting the modal's
  // onNewLink wiring would not fail this test on its own. What it does prove
  // is that the whole path -- confirm, mutate, re-render, inside a real
  // <dialog> with the real hooks -- works, and that useNewInviteLink's own
  // invalidation finds a registered GET /invites rather than throwing.
  it("keeps the card showing the invite link after minting a new one", async () => {
    renderModal({
      features: { email_invites: false, telegram_sign_in: true },
      defaultRole: "owner",
      routes: {
        [`POST ${INVITE_URL}`]: {
          status: 201,
          body: { id: "1", expiresAt: "2026-09-21T00:00:00Z", link: "https://t.me/HearthBot?start=inv_old" },
        },
        [`GET ${INVITES_URL}`]: { status: 200, body: [] },
        [`POST ${INVITES_URL}/1/link`]: {
          status: 200,
          body: { link: "https://t.me/HearthBot?start=inv_new", expiresAt: "2026-09-22T00:00:00Z" },
        },
      },
    });

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Christine" } });
    fireEvent.click(screen.getByRole("button", { name: /send invite/i }));
    expect(await screen.findByText("https://t.me/HearthBot?start=inv_old")).toBeInTheDocument();

    // "Get a new link" is a two-click confirm (PendingInviteCard's own
    // NewLinkControl, an in-page confirm rather than window.confirm): the
    // first click asks, the second -- same label -- confirms.
    fireEvent.click(screen.getByRole("button", { name: /get a new link/i }));
    fireEvent.click(screen.getByRole("button", { name: /get a new link/i }));

    expect(await screen.findByText("https://t.me/HearthBot?start=inv_new")).toBeInTheDocument();
    expect(screen.queryByText("https://t.me/HearthBot?start=inv_old")).not.toBeInTheDocument();
  });
});
