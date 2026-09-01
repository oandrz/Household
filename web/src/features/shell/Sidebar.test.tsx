// Behaviours from the task-19 brief, updated by task 2 (Marriage and Family
// lost their SPACE_PAGES entries -- see Sidebar.tsx) and by task 10, which
// gave Marriage its own entry back (Retros):
// 1. A space present in `me.spaces` renders only if it has a SPACE_PAGES
//    entry, or is a custom (non-builtin) space -- the sidebar must render
//    from the payload, not a hard-coded list.
// 2. Given several spaces, all with something to render, they render in the
//    order the payload gives them (VisibleSpaces on the server already
//    sorted by position; the sidebar must not re-sort).
// 3. The footer shows the household name and a "Sign out" control.
// 4. Clicking "Sign out" calls POST /api/v1/auth/sign-out.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Me, Space } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { Sidebar } from "./Sidebar";

function space(overrides: Partial<Space>): Space {
  return {
    id: "space-id",
    key: "key",
    name: "Name",
    visibility: "everyone",
    position: 0,
    isBuiltin: true,
    ...overrides,
  };
}

const familySpace = space({
  id: "space-family",
  key: "family",
  name: "Family",
  position: 3,
});

const moneySpace = space({
  id: "space-money",
  key: "money",
  name: "Money",
  position: 1,
  requiredCapability: "money",
});

const marriageSpace = space({
  id: "space-marriage",
  key: "marriage",
  name: "Marriage",
  visibility: "parents_only",
  position: 2,
  requiredCapability: "marriage",
});

// A space a household made for itself with "+ New space". It has no
// SPACE_PAGES entry either, which is exactly why it is here: the rule below
// must key off isBuiltin, not off the absence of an entry.
const travelSpace = space({
  id: "space-travel",
  key: "travel",
  name: "Travel",
  position: 4,
  isBuiltin: false,
});

// overrides defaults to {} rather than gaining a second parallel fixture
// function -- Task 10's admin-link tests need everything this fixture
// already builds, plus one flipped bit.
function meFixture(spaces: Space[], overrides: Partial<Me> = {}): Me {
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
    spaces,
    isPlatformAdmin: false,
    features: {},
    ...overrides,
  };
}

describe("Sidebar", () => {
  // The chip advertised a command palette that does not exist -- the file
  // said so itself -- on desktop and inside the mobile drawer alike, on
  // devices with no ⌘ key. A palette is a feature and gets its own slice;
  // until then the app must not promise it.
  it("does not advertise a command palette", async () => {
    renderWithRouter(<Sidebar me={meFixture([moneySpace])} />);

    // The brand row proves the header actually rendered, so the negative
    // assertion below means the chip's absence and not the sidebar's.
    expect(await screen.findByText("Hearth")).toBeInTheDocument();
    expect(screen.queryByText("⌘K")).toBeNull();
  });

  it("renders no row for a builtin space whose pages are not built yet", async () => {
    // Family only -- Marriage got its own SPACE_PAGES entry back in task 10
    // (see the tests below), so it can no longer stand in for "a builtin
    // space with nothing built yet." Family is the only one left in that
    // state (docs/FEATURE_TRACKER.md section 7).
    renderWithRouter(
      <Sidebar me={meFixture([moneySpace, familySpace])} />,
    );

    // Money's own group renders, so the sidebar did mount -- without this the
    // assertions below would pass on an empty render.
    expect(await screen.findByText("Finances")).toBeInTheDocument();
    expect(screen.queryByText("Family")).toBeNull();
  });

  // Task 10: Marriage's own SPACE_PAGES entry, restored alongside its route
  // and RequireCapability guard (router.tsx's own header comment on why all
  // three land together), with a single Retros link at first. Task 11 added
  // Vision & goals as the group's second link -- SpaceLink's grouped branch
  // already handles any page count identically (SPACE_PAGES's own comment
  // on `length === 1` vs `length > 1`), so this needed no new rendering
  // logic, only the entry.
  it("renders a Marriage group with Retros and Vision & goals links for a member holding the capability", async () => {
    renderWithRouter(<Sidebar me={meFixture([marriageSpace])} />);

    expect(await screen.findByTestId("sidebar-space-label")).toHaveTextContent("Marriage");
    const retros = screen.getByRole("link", { name: "Retros" });
    expect(retros).toHaveAttribute("href", "/marriage/retros");
    const vision = screen.getByRole("link", { name: "Vision & goals" });
    expect(vision).toHaveAttribute("href", "/marriage/vision");
  });

  // The server -- not this component -- decides who sees Marriage at all:
  // VisibleSpaces filters `me.spaces` by capability before it ever reaches
  // the sidebar (this file's own header comment, point 1), so a member
  // without the capability simply never has marriageSpace in their array.
  // This pins that the sidebar renders nothing for a space it was never
  // given, rather than performing a second, redundant capability check of
  // its own that could disagree with the server's.
  it("renders no Marriage entry for a member without the capability", async () => {
    renderWithRouter(<Sidebar me={meFixture([familySpace])} />);

    expect(screen.queryByText("Marriage")).toBeNull();
    expect(screen.queryByRole("link", { name: "Retros" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Vision & goals" })).not.toBeInTheDocument();
  });

  it("still renders a space the household created, which has no built pages either", async () => {
    renderWithRouter(<Sidebar me={meFixture([moneySpace, travelSpace])} />);

    expect(await screen.findByText("Travel")).toBeInTheDocument();
  });

  it("renders only the spaces present in me.spaces", async () => {
    // travelSpace, not another builtin: `money` is the only builtin key left
    // in SPACE_PAGES, so two builtin spaces cannot show that this renders
    // from the payload rather than from a list in this file.
    renderWithRouter(<Sidebar me={meFixture([travelSpace])} />);

    expect(await screen.findByText("Travel")).toBeInTheDocument();
    expect(screen.queryByText("Finances")).toBeNull();
  });

  it("renders spaces in the order given, not re-sorted by position", async () => {
    // Fix round 1, Finding 2: the previous version of this test supplied
    // Money/Marriage/Family both in the array in that order AND with
    // ascending positions (1, 2, 3) -- a component that re-sorted by
    // `position` would have produced an identical rendered order, so the
    // test could not have caught that regression. Task 2 removed Marriage
    // and Family's SPACE_PAGES entries, so a builtin space can no longer
    // stand in for "renders, but not as Money's group" -- only a custom
    // space still does (see SPACE_PAGES's comment). This supplies two
    // custom spaces and Money in the array as Travel, Money, Garden while
    // their `position` fields are 3, 1, 2 respectively (still distinct and
    // still meaningful data, just not ascending in array order), and
    // asserts the *array* order is what renders -- not the position-sorted
    // order (which would be Money, Garden, Travel). Per
    // domain.VisibleSpaces's own documented invariant, the server has
    // already ordered by position and the sidebar must render exactly what
    // it's given, never re-sort.
    const outOfOrderTravel = space({
      id: "space-travel",
      key: "travel",
      name: "Travel",
      position: 3,
      isBuiltin: false,
    });
    const outOfOrderMoney = space({
      id: "space-money",
      key: "money",
      name: "Money",
      position: 1,
      requiredCapability: "money",
    });
    const outOfOrderGarden = space({
      id: "space-garden",
      key: "garden",
      name: "Garden",
      position: 2,
      isBuiltin: false,
    });

    renderWithRouter(
      <Sidebar
        me={meFixture([outOfOrderTravel, outOfOrderMoney, outOfOrderGarden])}
      />,
    );

    // Money now expands into a group label plus one row per built page
    // (Finances, Transactions, Budget, Goals, Bills), so the flat "Travel,
    // Money, Garden" this test would otherwise assert no longer matches --
    // the grouped shape adds a label plus five link rows for Money alone.
    // "sidebar-space" tags both links and the custom-space fallback span;
    // the group label has its own "sidebar-space-label" testid (finding 5)
    // -- asserted here as row order and label presence separately, so this
    // test's actual purpose (payload order, not position-sorted) isn't
    // entangled with where a non-link label happens to land in a flattened
    // query.
    const links = await screen.findAllByTestId("sidebar-space");
    expect(links.map((el) => el.textContent)).toEqual([
      "Travel",
      "Finances",
      "Transactions",
      "Budget",
      "Goals",
      "Bills",
      "Garden",
    ]);
    expect(screen.getByTestId("sidebar-space-label")).toHaveTextContent("Money");
  });

  // Bills' own Task 11: the design's own order (Finances, Transactions,
  // Budget, Goals, Bills) -- Bills joins as the fifth link, after Goals, now
  // that its own route and page exist.
  it("groups Money into a label with Finances, Transactions, Budget, Goals and Bills links, in that order", async () => {
    stubFetchRoutes({});
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
    );
    // The group label is not a link.
    expect(await screen.findByText("Money")).toBeInTheDocument();
    expect(screen.getByText("Money").closest("a")).toBeNull();
    const links = screen.getAllByTestId("sidebar-space");
    expect(links.map((el) => el.textContent)).toEqual([
      "Finances",
      "Transactions",
      "Budget",
      "Goals",
      "Bills",
    ]);
    expect(screen.getByRole("link", { name: "Finances" })).toHaveAttribute("href", "/money");
    expect(screen.getByRole("link", { name: "Transactions" })).toHaveAttribute(
      "href",
      "/money/transactions",
    );
    expect(screen.getByRole("link", { name: "Budget" })).toHaveAttribute(
      "href",
      "/money/budget",
    );
    expect(screen.getByRole("link", { name: "Goals" })).toHaveAttribute(
      "href",
      "/money/goals",
    );
    expect(screen.getByRole("link", { name: "Bills" })).toHaveAttribute(
      "href",
      "/money/bills",
    );
  });

  it("renders an unrecognised space key as plain text, not a link", async () => {
    stubFetchRoutes({});
    renderWithRouter(
      <Sidebar
        me={meFixture([
          // "+ New space" (Task 20) can let a household create a space
          // whose key SPACE_PAGES doesn't recognise yet -- that must render
          // as inert text rather than a Link to a route that doesn't exist.
          // isBuiltin: false, because the same absent-entry branch now
          // renders nothing at all for a builtin space (see SPACE_PAGES's
          // comment) -- only a custom space still falls through to text.
          space({
            id: "space-budget",
            key: "budget",
            name: "Budget",
            position: 1,
            isBuiltin: false,
          }),
        ])}
      />,
    );

    const budget = await screen.findByTestId("sidebar-space");
    expect(budget).toHaveTextContent("Budget");
    expect(budget.tagName).not.toBe("A");
    expect(screen.queryByRole("link", { name: "Budget" })).not.toBeInTheDocument();
  });

  // The jsdom-visible half of the cascade bug fixed in e860d89: an active
  // link must carry `text-accent` and *not* `text-ink` at the same time --
  // `toHaveClass("text-accent")` alone would have stayed green throughout
  // that whole defect, since the broken markup carried both tokens on every
  // link and the class-string assertion never noticed which one a real
  // cascade actually painted. Asserting the absence of the other color
  // class is what makes this test able to fail again.
  it("accents only the active Money link", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    const goals = screen.getByRole("link", { name: "Goals" });
    expect(finances).toHaveClass("text-accent");
    expect(finances).not.toHaveClass("text-ink");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
    expect(budget).toHaveClass("text-ink");
    expect(budget).not.toHaveClass("text-accent");
    expect(goals).toHaveClass("text-ink");
    expect(goals).not.toHaveClass("text-accent");
  });

  // Task 11's own regression test for the activeProps cascade defect
  // (LEARNING pattern 3): Budget's active state has to come from the same
  // route-driven `useMatchRoute` computation as its siblings, not from
  // `Link`'s `activeProps` (which merges rather than replaces className and
  // shipped the bug the comment above describes). Asserts both halves --
  // Budget carries the accent and not the ink class while its siblings carry
  // the reverse -- the same shape as "accents only the active Money link".
  it("accents only the active Budget link, on /money/budget", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money/budget",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    expect(budget).toHaveClass("text-accent");
    expect(budget).not.toHaveClass("text-ink");
    expect(finances).toHaveClass("text-ink");
    expect(finances).not.toHaveClass("text-accent");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
  });

  // Task 10's own regression test for the activeProps cascade defect
  // (LEARNING pattern 3), Goals' turn: its active state has to come from the
  // same route-driven `useMatchRoute` computation as its siblings, not from
  // `Link`'s `activeProps`, which merges rather than replaces className and
  // shipped the bug this comment describes. Asserts both halves -- Goals
  // carries the accent and not the ink class while its siblings carry the
  // reverse -- the same shape as "accents only the active Budget link".
  it("accents only the active Goals link, on /money/goals", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money/goals",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    const goals = screen.getByRole("link", { name: "Goals" });
    expect(goals).toHaveClass("text-accent");
    expect(goals).not.toHaveClass("text-ink");
    expect(finances).toHaveClass("text-ink");
    expect(finances).not.toHaveClass("text-accent");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
    expect(budget).toHaveClass("text-ink");
    expect(budget).not.toHaveClass("text-accent");
  });

  // Bills' own Task 11 regression test for the identical activeProps
  // cascade defect (LEARNING pattern 3), Bills' turn -- the same shape as
  // "accents only the active Goals link".
  it("accents only the active Bills link, on /money/bills", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money/bills",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    const goals = screen.getByRole("link", { name: "Goals" });
    const bills = screen.getByRole("link", { name: "Bills" });
    expect(bills).toHaveClass("text-accent");
    expect(bills).not.toHaveClass("text-ink");
    expect(finances).toHaveClass("text-ink");
    expect(finances).not.toHaveClass("text-accent");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
    expect(budget).toHaveClass("text-ink");
    expect(budget).not.toHaveClass("text-accent");
    expect(goals).toHaveClass("text-ink");
    expect(goals).not.toHaveClass("text-accent");
  });

  // Overview used to be unconditionally text-accent (finding 3): it stayed
  // green even on /settings, where nothing about it was actually active.
  it("accents Overview only on /, not on another route", async () => {
    renderWithRouter(<Sidebar me={meFixture([])} />, "/settings");

    const overview = await screen.findByRole("link", { name: "Overview" });
    expect(overview).toHaveClass("text-ink");
    expect(overview).not.toHaveClass("text-accent");
  });

  // Settings used to be unconditionally text-ink (finding 3): it never
  // accented even while on /settings itself.
  it("accents Settings only on /settings", async () => {
    renderWithRouter(<Sidebar me={meFixture([])} />, "/settings");

    const settings = await screen.findByRole("link", { name: "Settings" });
    expect(settings).toHaveClass("text-accent");
    expect(settings).not.toHaveClass("text-ink");
  });

  it("shows the household name and a Sign out control in the footer", async () => {
    renderWithRouter(<Sidebar me={meFixture([])} />);

    expect(await screen.findByText("Andreas & Christine")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out/i })).toBeInTheDocument();
  });

  it("calls POST /api/v1/auth/sign-out when Sign out is clicked", async () => {
    const fetchMock = stubFetchRoutes({
      "POST /api/v1/auth/sign-out": { status: 204, body: undefined },
      "GET /api/v1/auth/me": {
        status: 401,
        body: { error: { code: "UNAUTHENTICATED", message: "Not signed in." } },
      },
    });

    renderWithRouter(<Sidebar me={meFixture([])} />);
    fireEvent.click(await screen.findByRole("button", { name: /sign out/i }));

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/auth/sign-out" &&
            (init?.method ?? "GET").toUpperCase() === "POST",
        ),
      ).toBe(true);
    });
  });

  // The Admin link is not a space -- it does not go through SPACE_PAGES or
  // me.spaces at all, so it needs its own pair of tests: gone for the
  // ordinary case, present only for the one flag that governs it.
  it("does not show an Admin link for an ordinary owner", async () => {
    renderWithRouter(<Sidebar me={meFixture([])} />);

    expect(await screen.findByText("Andreas & Christine")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Admin" })).not.toBeInTheDocument();
  });

  it("shows an Admin link for a platform admin", async () => {
    renderWithRouter(
      <Sidebar me={meFixture([], { isPlatformAdmin: true })} />,
    );

    expect(await screen.findByRole("link", { name: "Admin" })).toBeInTheDocument();
  });
});
