// Create a personal API token, then show its secret exactly once (ADR 7
// rule 7; PRD item 14). The secret lives only in this component's mutation
// state: closing the dialog resets it, and useCreateApiToken's own
// `gcTime: 0` (useHouseholdAccess.ts) stops TanStack's MutationCache from
// holding onto the settled mutation -- secret included -- for its default
// five minutes after that reset. Nothing else holds it: not the query
// cache (a create is invalidated, never cached), not storage.
//
// Copy only. No QR and no Share-to-Telegram, unlike InviteLinkShare.tsx: an
// invite link is meant for another person; a token is a long-lived
// credential for the member's own scripts and should not travel.
import { useId, useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { MAX_TOKEN_NAME_LENGTH, TOKEN_LIFETIME_OPTIONS } from "./copy";
import { useCreateApiToken, type NewApiToken } from "./useHouseholdAccess";

const DEFAULT_DAYS = 90;

export function NewApiTokenModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nameId = useId();
  const lifetimeId = useId();
  const [name, setName] = useState("");
  const [days, setDays] = useState<number>(DEFAULT_DAYS);
  const [copied, setCopied] = useState(false);
  const create = useCreateApiToken();

  function close() {
    setName("");
    setDays(DEFAULT_DAYS);
    setCopied(false);
    create.reset(); // drops the secret from the mutation's own state
    onClose();
  }

  async function copy(secret: string) {
    // Guarded for the reason InviteLinkShare.tsx gives: no clipboard on
    // plain HTTP or in jsdom. The secret stays on screen to copy by hand.
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(secret);
    setCopied(true);
  }

  const created = create.data;

  return (
    <Modal open={open} onClose={close} title={created ? "Your new token" : "New API token"}>
      {created ? (
        <div className="flex flex-col gap-3 text-[13px]">
          <p className="text-ink">
            Copy it now. <span className="font-semibold">You won't see it again</span> — Hearth keeps only a
            fingerprint of it.
          </p>
          <code className="block break-all rounded-lg border border-hairline bg-canvas p-3 font-mono text-[12px] text-ink">
            {created.token}
          </code>
          <ModalActions
            secondaryLabel={copied ? "Copied" : "Copy"}
            onSecondary={() => void copy(created.token)}
            primaryLabel="Done"
            primaryType="button"
            onPrimary={close}
          />
        </div>
      ) : (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            const input: NewApiToken = { name: name.trim(), expiresInDays: days };
            create.mutate(input);
          }}
        >
          <Field label="Name" htmlFor={nameId}>
            <input
              id={nameId}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. laptop script"
              className={FIELD_CONTROL_CLASS}
              maxLength={MAX_TOKEN_NAME_LENGTH}
            />
          </Field>
          <Field label="Expires after" htmlFor={lifetimeId}>
            <select
              id={lifetimeId}
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              className={FIELD_CONTROL_CLASS}
            >
              {TOKEN_LIFETIME_OPTIONS.map((o) => (
                <option key={o.days} value={o.days}>
                  {o.label}
                </option>
              ))}
            </select>
          </Field>
          {create.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(create.error, "Couldn't create that token. Please try again.")}
            </p>
          )}
          <ModalActions
            secondaryLabel="Cancel"
            onSecondary={close}
            primaryLabel="Create token"
            primaryType="submit"
            primaryDisabled={name.trim() === "" || create.isPending}
          />
        </form>
      )}
    </Modal>
  );
}
