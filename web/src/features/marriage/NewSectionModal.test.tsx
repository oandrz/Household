import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { NewSectionModal } from "./NewSectionModal";

const EMPTY = {
  locked: false,
  owners: [
    { membershipId: "m-a", name: "Andreas" },
    { membershipId: "m-c", name: "Christine" },
  ],
  version: 1,
  updatedAt: null,
  sections: [],
  proposals: [],
  history: [],
};

const NEW_SECTION = { id: "s-new", name: "Faith & values", count: 0, visible: false, agreements: [] };

function renderNewSection(routes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  // `names` is this test's own addition, beyond the brief's `posts.count`:
  // a chip whose stale closure fires the write anyway still lands at exactly
  // one POST (Create is already disabled by then, so the second click the
  // test fires is a no-op) -- the count alone cannot tell that apart from the
  // one correct POST. What it sent can: the mutated chip posts `{name: ""}`,
  // never the name on screen.
  const posts = { count: 0, names: [] as string[] };
  const onCreated = vi.fn();
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": { status: 200, body: { agreements: EMPTY } },
    "POST /api/v1/marriage/agreements/sections": {
      status: 201,
      body: { section: NEW_SECTION, agreements: { ...EMPTY, sections: [NEW_SECTION] } },
      capture: (body) => {
        posts.count += 1;
        posts.names.push((body as { name: string }).name);
      },
    },
    ...routes,
  });
  renderWithRouter(<NewSectionModal onCreated={onCreated} onClose={() => {}} />);
  return { posts, onCreated };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("NewSectionModal", () => {
  // The chips FILL the input (spec, The modals). One that submitted would make
  // a single click an unsigned write with the wrong name on it.
  it("a suggestion chip fills the input, and Create seeds an add on the new section", async () => {
    const { posts, onCreated } = renderNewSection();

    fireEvent.click(await screen.findByRole("button", { name: "Faith & values" }));
    expect(screen.getByLabelText("Section name")).toHaveValue("Faith & values");

    fireEvent.click(screen.getByRole("button", { name: "Create & add first agreement" }));
    await waitFor(() =>
      expect(onCreated).toHaveBeenCalledWith({ mode: "add", sectionId: "s-new" }),
    );

    // Exactly one POST, counted AFTER the Create has landed. Asserting `0`
    // straight after the chip click would prove nothing: mutateAsync awaits its
    // own hooks before it ever calls fetch, so the counter is still 0 on the
    // next line whatever the chip did.
    expect(posts.count).toBe(1);
    // And it carries the name the person saw and confirmed by clicking
    // Create -- not whatever the chip's onClick closed over. A chip that
    // ALSO called handleCreate would send this POST itself, one render
    // before setName("Faith & values") has committed, so its body would
    // read {name: ""} even though posts.count stayed at 1 (Create is
    // already disabled for the test's own click by then, so no second POST
    // ever fires to raise the count).
    expect(posts.names).toEqual(["Faith & values"]);
  });

  // Decision 19's mapping, under the field: the modal stays open and the typed
  // name survives, or this is the empty-<select> dead end again -- a generic
  // red box four clicks in (criterion 13).
  it("a duplicate name says so under the field, keeping the modal and the name", async () => {
    renderNewSection({
      "POST /api/v1/marriage/agreements/sections": {
        status: 409,
        body: {
          error: { code: "AGREEMENT_SECTION_NAME_TAKEN", message: "That section already exists." },
        },
      },
    });

    fireEvent.change(await screen.findByLabelText("Section name"), {
      target: { value: "Money" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create & add first agreement" }));

    // Substring, deliberately: the sentence is asserted, its trailing full stop
    // belongs to Task 9's own copy value and is not this test's to pin.
    expect(await screen.findByTestId("agreement-section-name-error")).toHaveTextContent(
      "You already have a section called that",
    );
    expect(screen.getByLabelText("Section name")).toHaveValue("Money");
    expect(screen.getByRole("heading", { name: "New agreement section" })).toBeInTheDocument();
  });
});
