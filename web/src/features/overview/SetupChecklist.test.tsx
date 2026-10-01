// The checklist names a month ("Set a budget for October"), and the month is
// the household's. The browser here is in another zone on purpose.
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { meRoute } from "../../test/meFixture";
import { SetupChecklist } from "./SetupChecklist";

const ORIGINAL_TZ = process.env.TZ;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

describe("SetupChecklist", () => {
  // A household that opens the app on its own 1 October must not be told to
  // budget for September. At 23:00 UTC on 30 September that is exactly what a
  // browser in Los Angeles, or a UTC clock, would say.
  it("names the household's month in the budget step, whatever zone the browser is in", async () => {
    process.env.TZ = "America/Los_Angeles";
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));
    stubFetchRoutes(meRoute("Asia/Singapore"));

    renderWithRouter(<SetupChecklist hasAccount={false} hasBudget={false} partner="none" />);

    await waitFor(() => expect(screen.getByText("Set a budget for October")).toBeInTheDocument());
    expect(screen.queryByText("Set a budget for September")).not.toBeInTheDocument();
  });
});
