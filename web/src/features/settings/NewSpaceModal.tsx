// The "New space" modal (design/Household Dashboard.dc.html's Settings
// screen, the modalSpace panel). Owner-only, same reasoning as
// InviteMemberModal: this screen never renders its trigger for anyone else
// (see SpacesPanel.tsx), and POST /spaces sits behind requireOwner
// regardless.
import { type FormEvent, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Modal } from "../../components/Modal";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { spaceSchema } from "../auth/schemas";

type VisibilityOption = "everyone" | "parents_only";

// The design's four template tiles. Selecting one prefills the Name field
// (the design shows "Kids" selected with Name already reading "Kids") --
// "template" itself is otherwise inert: HouseholdService.CreateSpace has no
// notion of a template, only a name and a visibility (see
// household_handlers.go's createSpaceRequest comment). Sending it is
// harmless and matches the request shape the API spec documents.
const TEMPLATES: { key: string; name: string; blurb: string }[] = [
  { key: "kids", name: "Kids", blurb: "Chores, allowance, school, health" },
  { key: "home", name: "Home", blurb: "Maintenance, warranties, projects" },
  { key: "travel", name: "Travel", blurb: "Trips, packing, itineraries" },
  { key: "blank", name: "", blurb: "Start empty, add your own pages" },
];

async function createSpace(vars: {
  name: string;
  visibility: VisibilityOption;
  template: string;
}) {
  const body = await apiFetch<unknown>("/api/v1/spaces", {
    method: "POST",
    body: JSON.stringify(vars),
  });
  return spaceSchema.parse(body);
}

function useCreateSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createSpace,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["spaces"] });
      queryClient.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export function NewSpaceModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [template, setTemplate] = useState("kids");
  const [name, setName] = useState("Kids");
  const [visibility, setVisibility] = useState<VisibilityOption>("everyone");
  const createSpaceMutation = useCreateSpace();

  function selectTemplate(key: string) {
    setTemplate(key);
    const found = TEMPLATES.find((t) => t.key === key);
    setName(found?.name ?? "");
  }

  function reset() {
    setTemplate("kids");
    setName("Kids");
    setVisibility("everyone");
    createSpaceMutation.reset();
  }

  function handleClose() {
    reset();
    onClose();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createSpaceMutation.mutate(
      { name, visibility, template },
      { onSuccess: handleClose },
    );
  }

  return (
    <Modal open={open} onClose={handleClose} title="New space">
      <p className="-mt-3 mb-5 text-xs text-muted">
        A space groups related pages in the sidebar
      </p>

      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-2.5">
          <span className="text-xs font-semibold text-label">Start from a template</span>
          <div className="grid grid-cols-2 gap-2.5">
            {TEMPLATES.map((t) => (
              <button
                key={t.key}
                type="button"
                onClick={() => selectTemplate(t.key)}
                aria-pressed={template === t.key}
                className={`rounded-[10px] border p-3.5 text-left ${
                  template === t.key ? "border-accent bg-callout" : "border-hairline"
                }`}
              >
                <div className="text-[13.5px] font-semibold text-ink">
                  {t.key === "blank" ? "Blank space" : t.name}
                </div>
                <div className="mt-0.5 text-[11.5px] text-muted">{t.blurb}</div>
              </button>
            ))}
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="new-space-name" className="text-xs font-semibold text-label">
            Name
          </label>
          <input
            id="new-space-name"
            type="text"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex flex-col gap-2">
          <span className="text-xs font-semibold text-label">Who can see it</span>
          <div className="flex gap-1.5">
            <button
              type="button"
              onClick={() => setVisibility("everyone")}
              aria-pressed={visibility === "everyone"}
              // min-h-11/sm:min-h-0: py-2.5 alone measured short of the 44px
              // floor at this text size -- TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured numbers.
              className={`min-h-11 rounded-full px-3.5 py-2.5 text-[12.5px] font-semibold sm:min-h-0 sm:py-1.5 ${
                visibility === "everyone" ? "bg-accent text-white" : "border border-hairline text-label"
              }`}
            >
              Everyone
            </button>
            <button
              type="button"
              onClick={() => setVisibility("parents_only")}
              aria-pressed={visibility === "parents_only"}
              className={`min-h-11 rounded-full px-3.5 py-2.5 text-[12.5px] font-semibold sm:min-h-0 sm:py-1.5 ${
                visibility === "parents_only" ? "bg-accent text-white" : "border border-hairline text-label"
              }`}
            >
              Parents only
            </button>
            {/* usecase.ErrSpaceVisibilityNotSupported: POST /spaces accepts
                only "everyone" and "parents_only" -- "custom" is rejected
                outright. Rendering it as a selectable pill that silently
                behaved like Everyone would be worse than not offering it;
                disabled, with the design's own "· not built" marker (used
                elsewhere in this document for exactly this situation). */}
            <button
              type="button"
              disabled
              aria-pressed={false}
              className="min-h-11 rounded-full border border-hairline px-3.5 py-2.5 text-[12.5px] font-semibold text-muted disabled:cursor-not-allowed sm:min-h-0 sm:py-1.5"
            >
              Custom <span className="text-[10px] font-medium">· not built</span>
            </button>
          </div>
        </div>

        {createSpaceMutation.isError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {apiErrorMessage(createSpaceMutation.error, "Something went wrong. Please try again.")}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={handleClose}
            className="flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={createSpaceMutation.isPending}
            className="flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            Create space
          </button>
        </div>
      </form>
    </Modal>
  );
}
