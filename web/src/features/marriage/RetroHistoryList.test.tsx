// fireEvent, not @testing-library/user-event: the latter is not a
// dependency anywhere in this codebase (SignUpScreen.test.tsx's own comment
// on the same point), so this matches that convention rather than
// introducing a new one for a single file.
//
// stubFetchRoutes is not used here -- RetroHistoryList takes its data as
// props and makes no request of its own. The "fetches nothing to expand"
// test below proves that directly: no fetch stub is installed at all, so
// any call to the real `fetch` would throw (jsdom has none configured),
// failing the test on its own rather than needing a route registry to catch
// it.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RetroHistoryList } from "./RetroHistoryList";
import type { RetroSummary } from "./retroSchemas";

function summaryFixture(overrides: Partial<RetroSummary> = {}): RetroSummary {
  return {
    id: `retro-${overrides.month ?? "2026-06"}`,
    month: "2026-06",
    mood: 4,
    actionCount: 3,
    // openActionCount is not read by this component -- RetroHistoryList
    // renders the TOTAL (spec's formulas table: "K counts all of that
    // retro's actions, ticked or not"), unlike Overview's NextRetroCard --
    // present here only because retroSummarySchema requires it on the wire.
    openActionCount: 1,
    quote: "best month this year",
    finished: true,
    ...overrides,
  };
}

// Twelve 2026 rows, newest first -- matches ListRetros' own "ORDER BY
// r.month DESC" (retro_repo.go), which RetroHistoryList.tsx's grouping
// relies on to keep same-year rows contiguous without re-sorting.
function twelve2026(): RetroSummary[] {
  return Array.from({ length: 12 }, (_, i) => {
    const monthNum = String(12 - i).padStart(2, "0");
    return summaryFixture({ id: `retro-2026-${monthNum}`, month: `2026-${monthNum}` });
  });
}

// Seven 2025 rows, also newest first -- the design's own "(7 more)" count.
function seven2025(): RetroSummary[] {
  return Array.from({ length: 7 }, (_, i) => {
    const monthNum = String(12 - i).padStart(2, "0");
    return summaryFixture({ id: `retro-2025-${monthNum}`, month: `2025-${monthNum}` });
  });
}

describe("RetroHistoryList", () => {
  // The design's own row: `June 2026 · Mood 4/5 · 3 actions · "best month
  // this year"`. Each clause disappears when it has nothing to say -- never
  // "0 actions", never empty quotation marks (task-11-brief.md). Task 10's
  // own stand-in got the action clause wrong (rendered unconditionally),
  // which is exactly the defect family this test exists to catch.
  it("renders only the clauses a retro actually has", () => {
    render(
      <RetroHistoryList
        summaries={[
          summaryFixture({ id: "retro-june", month: "2026-06", mood: 4, actionCount: 3, quote: "best month this year" }),
          summaryFixture({ id: "retro-may", month: "2026-05", mood: null, actionCount: 0, quote: "" }),
        ]}
        onSelect={() => {}}
        selectedMonth="2026-06"
      />,
    );

    const rich = screen.getByTestId("retro-row-2026-06");
    expect(rich).toHaveTextContent("Mood 4/5");
    expect(rich).toHaveTextContent("3 actions");
    expect(rich).toHaveTextContent("best month this year");

    const bare = screen.getByTestId("retro-row-2026-05");
    expect(bare).not.toHaveTextContent("Mood");
    expect(bare).not.toHaveTextContent("actions");
    expect(bare.textContent).not.toContain('""');
  });

  // A single action still reads "1 action", not "1 actions" -- the same
  // pluralisation guard the done-count clause already carries elsewhere.
  it("singularises exactly one action", () => {
    render(
      <RetroHistoryList
        summaries={[summaryFixture({ month: "2026-06", actionCount: 1 })]}
        onSelect={() => {}}
        selectedMonth={null}
      />,
    );

    expect(screen.getByTestId("retro-row-2026-06")).toHaveTextContent("1 action");
    expect(screen.getByTestId("retro-row-2026-06")).not.toHaveTextContent("1 actions");
  });

  // A draft's row shows only the in-progress label, regardless of whatever
  // mood/actionCount/quote a schema-legal response might carry alongside
  // finished: false -- the design's rule is about a *finished* retro's
  // clauses, and a draft is not one.
  it("shows a draft as in progress, never its clauses", () => {
    render(
      <RetroHistoryList
        summaries={[summaryFixture({ month: "2026-08", finished: false, mood: 5, actionCount: 2, quote: "irrelevant" })]}
        onSelect={() => {}}
        selectedMonth={null}
      />,
    );

    const row = screen.getByTestId("retro-row-2026-08");
    expect(row).toHaveTextContent("In progress");
    expect(row).not.toHaveTextContent("Mood");
    expect(row).not.toHaveTextContent("irrelevant");
  });

  // "Show 2025 (7 more)" is a disclosure over data the page already
  // holds, not a second request -- no fetch stub is installed anywhere in
  // this file, so an accidental fetch call would throw and fail the test by
  // itself (this file's own header comment).
  it("collapses older years behind a disclosure and fetches nothing to expand them", () => {
    render(
      <RetroHistoryList summaries={[...twelve2026(), ...seven2025()]} onSelect={() => {}} selectedMonth="2026-06" />,
    );

    expect(screen.queryByTestId("retro-row-2025-12")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Show 2025 \(7 more\)/ }));
    expect(screen.getByTestId("retro-row-2025-12")).toBeInTheDocument();
    // All seven, not just the one row this test happened to check first.
    expect(screen.getByTestId("retro-row-2025-06")).toBeInTheDocument();
  });

  // Clicking a row is how Task 12's detail panel will learn which month to
  // show -- this is the one piece of real interactivity this component
  // ships, so it gets its own direct test rather than only the visual
  // "selectedMonth" prop above.
  it("calls onSelect with the clicked row's month", () => {
    const onSelect = vi.fn();
    render(
      <RetroHistoryList
        summaries={[summaryFixture({ month: "2026-06" }), summaryFixture({ month: "2026-05", finished: false })]}
        onSelect={onSelect}
        selectedMonth={null}
      />,
    );

    fireEvent.click(screen.getByTestId("retro-row-2026-05"));
    expect(onSelect).toHaveBeenCalledWith("2026-05");
  });
});
