// No fetch stub or router needed -- MilestoneGrid takes `milestones` as a
// prop (the same data VisionPage.tsx's own useVision(year) already fetched)
// and `onEdit` as a plain callback, not a Link -- MoodChart.test.tsx's own
// precedent for a pure-presentation component.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MilestoneGrid } from "./MilestoneGrid";
import type { VisionMilestone } from "./visionSchemas";

describe("MilestoneGrid", () => {
  it("renders milestones in order, each with its year, title and note", () => {
    const milestones: VisionMilestone[] = [
      { year: 2027, title: "Sabbatical month in Indonesia", note: "Kids meet Christine's side properly" },
      { year: 2029, title: "Upgrade to a bigger place", note: "Tied to car + housing goals" },
    ];

    render(<MilestoneGrid milestones={milestones} onEdit={() => {}} />);

    const cards = screen.getAllByTestId("vision-milestone-card");
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveTextContent("2027");
    expect(cards[0]).toHaveTextContent("Sabbatical month in Indonesia");
    expect(cards[0]).toHaveTextContent("Kids meet Christine's side properly");
    expect(cards[1]).toHaveTextContent("2029");
    expect(cards[1]).toHaveTextContent("Upgrade to a bigger place");
    expect(cards[1]).toHaveTextContent("Tied to car + housing goals");
  });

  // A milestone's own note is "" on the wire whenever a household didn't
  // write one -- the same "empty string renders nothing" rule the vision's
  // description and each pillar's own description follow.
  it("renders no note line when a milestone's note is empty", () => {
    render(
      <MilestoneGrid
        milestones={[{ year: 2032, title: "Education fund fully funded", note: "" }]}
        onEdit={() => {}}
      />,
    );

    expect(screen.queryByTestId("vision-milestone-note")).not.toBeInTheDocument();
  });

  it("renders the panel heading and the + Add milestone affordance even with no milestones yet", () => {
    render(<MilestoneGrid milestones={[]} onEdit={() => {}} />);

    expect(screen.getByText("Longer horizon")).toBeInTheDocument();
    expect(screen.getByTestId("vision-add-milestone")).toHaveTextContent("+ Add milestone");
    expect(screen.queryByTestId("vision-milestone-card")).not.toBeInTheDocument();
  });

  // The affordance opens the same editor the header's own Edit vision
  // button does (task-11's own ruling 2) -- this proves it is wired to
  // WHATEVER handler the caller passes, not hard-coded to do nothing.
  // VisionPage.tsx wires that handler as a no-op placeholder today;
  // Task 12 replaces it, and this test does not change when that happens.
  it("clicking + Add milestone calls the onEdit handler its caller supplied", () => {
    const onEdit = vi.fn();
    render(<MilestoneGrid milestones={[]} onEdit={onEdit} />);

    fireEvent.click(screen.getByTestId("vision-add-milestone"));

    expect(onEdit).toHaveBeenCalledTimes(1);
  });
});
