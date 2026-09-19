// The Vision editor's local draft -- theme, description, every pillar and
// every milestone -- together with the effect that seeds it from the loaded
// document, Save, and Reload-and-discard. Pulled out of VisionModal.tsx so
// the modal only renders; the state, handlers and comments below moved here
// unchanged. VisionModal.tsx's own header comment explains the conflict
// mechanism `handleSave` and `handleReloadAndDiscard` implement.
import { useEffect, useState } from "react";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../../api/errorMessage";
import type { SaveVisionBody, useVision } from "./useVision";
import { VISION_COPY } from "./visionCopy";
import type { DraftMilestone, DraftPillar } from "./visionDraft";

type Vision = ReturnType<typeof useVision>;

export function useVisionDraft({
  year,
  data,
  saveVision,
  reload,
  onClose,
}: {
  year: number;
  data: Vision["data"];
  saveVision: Vision["saveVision"];
  reload: Vision["reload"];
  onClose: () => void;
}) {
  const [theme, setTheme] = useState("");
  const [description, setDescription] = useState("");
  const [pillars, setPillars] = useState<DraftPillar[]>([]);
  const [milestones, setMilestones] = useState<DraftMilestone[]>([]);
  // Which year's document this draft was last seeded from -- null until the
  // first load. `data.year` is always present, even on the empty document
  // decision 9 returns for a year nobody has set (VisionService.Get's own
  // fallback carries `Year: year`, the year that was actually requested),
  // so this reseeds exactly once per year the household switches to and can
  // never seed one year's fields from another's data -- a simpler and more
  // robust key than a plain "have we ever seeded" boolean paired with its
  // own separate effect resetting it on every `year` change.
  const [seededYear, setSeededYear] = useState<number | null>(null);

  const [hadConflict, setHadConflict] = useState(false);
  const [isReloading, setIsReloading] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (!data || data.year === seededYear) return;
    setTheme(data.theme);
    setDescription(data.description);
    setPillars(
      data.pillars.map((p) => ({
        name: p.name,
        description: p.description,
        measures: p.measures.map((m) => ({
          label: m.label,
          // "broken" (a server-only kind -- measureKindSchema's own
          // comment) has nothing left to edit as either typed or linked: no
          // current/target the household ever set, and toMeasureView's own
          // default branch (api/internal/usecase/vision.go) never fills in
          // a goalId for this kind. Landing it as typed, with the same
          // blank-but-valid defaults setMeasureMode's own typed branch
          // uses, is the least surprising choice: an editable shape the
          // household must fill in themselves, not a silently resurrected
          // link to a goal that no longer resolves.
          //
          // KNOWN GAP (docs/LEARNING.md, "Vision's fifteen-criterion
          // browser walk"): this seeds a figure -- "0 of 1" -- the
          // household never typed, and a save that never opens this row
          // (editing only the theme, say) resends it unchanged, silently
          // converting "Goal removed" into a fabricated number. Dormant
          // only because Goals has no delete route to reach MeasureBroken
          // in the first place. handleSave's own `unresolvedLinkedMeasure`
          // check (added for the goal-required bug) does NOT catch this --
          // that check looks for kind === "linked" with no goalId, and this
          // measure lands here as "typed", not "linked". The fix when Goals
          // gains delete is not relaxing the domain (write strictly is
          // correct) but a third seeded state: an editable row that visibly
          // says "needs a goal or a number" and blocks Save until the
          // household resolves it one way or the other.
          kind: m.kind === "linked" ? "linked" : "typed",
          current: m.kind === "typed" ? m.current : 0,
          target: m.kind === "typed" ? m.target : m.kind === "broken" ? 1 : 0,
          goalId: m.kind === "linked" ? m.goalId : "",
        })),
      })),
    );
    setMilestones(data.milestones.map((m) => ({ year: m.year, title: m.title, note: m.note })));
    // A conflict or a leftover save error both describe the document THIS
    // draft was built against -- once a fresh one has just been seeded
    // (whether from the household's own Reload-and-discard, or simply
    // switching to a year that happens to already be cached), neither
    // means anything any more.
    setHadConflict(false);
    setSaveError(null);
    setSeededYear(data.year);
  }, [data, seededYear]);

  function currentBody(): SaveVisionBody {
    return {
      theme,
      description,
      pillars: pillars.map((p) => ({
        name: p.name,
        description: p.description,
        measures: p.measures.map((m) => ({
          label: m.label,
          kind: m.kind,
          current: m.current,
          target: m.target,
          goalId: m.goalId,
        })),
      })),
      milestones: milestones.map((m) => ({ year: m.year, title: m.title, note: m.note })),
    };
  }

  async function handleSave() {
    setSaveError(null);
    // The first of two client-side checks this modal makes before ever
    // reaching the server. This one: the empty-document path (decision 9)
    // seeds theme as "", so a brand-new household's very first Save would
    // otherwise round-trip a 422 for something checkable in three lines.
    // Hearth's own message, not the browser's -- this form carries no
    // `required` attribute anywhere, per the UI-polish round's own rule
    // that native validation is not this product's error surface.
    if (theme.trim() === "") {
      setSaveError(VISION_COPY.modalThemeRequired);
      return;
    }
    // The second: a measure switched to "A savings goal" (setMeasureMode's
    // linked branch) but never given one is neither typed nor linked --
    // Validate would refuse it as ErrVisionMeasureGoalRequired, but the
    // household's own click that reaches this state (switch the mode, then
    // Save without picking a goal) is only two clicks away and this modal's
    // only OTHER client-side check was the theme above, so the round trip
    // was the household's sole feedback until now. Named here rather than
    // left to the server round-trip.
    const unresolvedLinkedMeasure = pillars.some((p) =>
      p.measures.some((m) => m.kind === "linked" && m.goalId === ""),
    );
    if (unresolvedLinkedMeasure) {
      setSaveError(VISION_COPY.modalMeasureGoalRequired);
      return;
    }
    try {
      await saveVision(currentBody());
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.code === "VISION_CHANGED") {
        setHadConflict(true);
      } else {
        setSaveError(apiErrorMessage(err, VISION_COPY.modalSaveError));
      }
    }
  }

  // Always closes, whether or not the refetch itself succeeded -- this
  // control's entire point is discarding the local draft, and there is
  // nothing left for a failed refetch to protect once that has happened.
  // Whatever the household sees next (the page behind this modal, or a
  // freshly reopened one) is what decides whether that refetch needs
  // retrying, not this button.
  async function handleReloadAndDiscard() {
    setIsReloading(true);
    try {
      await reload();
    } finally {
      setIsReloading(false);
      onClose();
    }
  }

  function addPillar() {
    setPillars((prev) => [...prev, { name: "", description: "", measures: [] }]);
  }
  function updatePillar(index: number, next: DraftPillar) {
    setPillars((prev) => prev.map((p, i) => (i === index ? next : p)));
  }
  function removePillar(index: number) {
    setPillars((prev) => prev.filter((_, i) => i !== index));
  }

  function addMilestone() {
    // Seeded with the vision's own year, not today's -- a longer-horizon
    // milestone is usually a few years out, but starting from the document
    // being edited is a closer guess than always defaulting to whatever
    // year the household happens to be looking at right now.
    setMilestones((prev) => [...prev, { year, title: "", note: "" }]);
  }
  function updateMilestone(index: number, next: DraftMilestone) {
    setMilestones((prev) => prev.map((m, i) => (i === index ? next : m)));
  }
  function removeMilestone(index: number) {
    setMilestones((prev) => prev.filter((_, i) => i !== index));
  }

  return {
    theme,
    setTheme,
    description,
    setDescription,
    pillars,
    milestones,
    hadConflict,
    isReloading,
    saveError,
    handleSave,
    handleReloadAndDiscard,
    addPillar,
    updatePillar,
    removePillar,
    addMilestone,
    updateMilestone,
    removeMilestone,
  };
}
