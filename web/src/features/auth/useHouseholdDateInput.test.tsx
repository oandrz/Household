// The browser is in Los Angeles and the household in Singapore, at an instant
// when the two are on different dates. A date field must show the
// household's.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { meRoute } from "../../test/meFixture";
import { useHouseholdDateInput } from "./useHouseholdDateInput";

const ORIGINAL_TZ = process.env.TZ;

beforeEach(() => {
  process.env.TZ = "America/Los_Angeles";
  vi.useFakeTimers({ toFake: ["Date"] });
  // 16:00 on 30 September in Los Angeles, 07:00 on 1 October in Singapore.
  vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

function DateField({ initial }: { initial?: string }) {
  const [date, setDate] = useHouseholdDateInput(initial ?? null);
  return (
    <>
      <label htmlFor="date">Date</label>
      <input id="date" type="date" value={date} onChange={(event) => setDate(event.target.value)} />
      <button type="button" onClick={() => setDate(null)}>
        Reset
      </button>
    </>
  );
}

describe("useHouseholdDateInput", () => {
  it("defaults to the household's today, not the browser's", async () => {
    stubFetchRoutes(meRoute("Asia/Singapore"));
    renderWithRouter(<DateField />);

    // waitFor: the test mounts before GET /auth/me has answered, which the
    // app never does. The field must still end up on the household's day,
    // which is why the default is derived on every render.
    await waitFor(() => expect(screen.getByLabelText("Date")).toHaveValue("2026-10-01"));
  });

  it("keeps a date the form was opened with", async () => {
    stubFetchRoutes(meRoute("Asia/Singapore"));
    renderWithRouter(<DateField initial="2026-07-04" />);

    expect(await screen.findByLabelText("Date")).toHaveValue("2026-07-04");
  });

  it("keeps what the person picked, and goes back to today on reset", async () => {
    stubFetchRoutes(meRoute("Asia/Singapore"));
    renderWithRouter(<DateField />);
    const field = await screen.findByLabelText("Date");
    await waitFor(() => expect(field).toHaveValue("2026-10-01"));

    fireEvent.change(field, { target: { value: "2026-09-15" } });
    expect(field).toHaveValue("2026-09-15");

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(field).toHaveValue("2026-10-01");
  });

  // An emptied field is the person's choice and stays empty, so the form's
  // own "pick a date" message can fire. Only "never touched" means today.
  it("leaves a field the person emptied empty", async () => {
    stubFetchRoutes(meRoute("Asia/Singapore"));
    renderWithRouter(<DateField />);
    const field = await screen.findByLabelText("Date");

    fireEvent.change(field, { target: { value: "" } });
    expect(field).toHaveValue("");
  });
});
