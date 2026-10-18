// Change password: current and new. On success this browser stays signed in
// on a fresh session and every other way in is revoked (POST /auth/password).
import { useId, useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { useChangePassword } from "../auth/useAuth";

export function ChangePasswordForm() {
  const currentId = useId();
  const nextId = useId();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const change = useChangePassword();

  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        change.mutate(
          { currentPassword: current, newPassword: next },
          {
            onSuccess: () => {
              setCurrent("");
              setNext("");
            },
          },
        );
      }}
    >
      <Field label="Current password" htmlFor={currentId}>
        <input id={currentId} type="password" autoComplete="current-password" value={current}
          onChange={(e) => setCurrent(e.target.value)} className={FIELD_CONTROL_CLASS} />
      </Field>
      <Field label="New password" htmlFor={nextId}>
        <input id={nextId} type="password" autoComplete="new-password" value={next} minLength={12}
          onChange={(e) => setNext(e.target.value)} className={FIELD_CONTROL_CLASS} />
      </Field>
      <p className="text-xs text-muted">
        Every other device is signed out, your API tokens are deleted and your Telegram chat is
        disconnected. This browser stays signed in.
      </p>
      {change.isError && (
        <p role="alert" className="text-[11px] text-danger">
          {apiErrorMessage(change.error, "Couldn't change your password. Please try again.")}
        </p>
      )}
      {change.isSuccess && <p className="text-[11px] text-ink">Password changed. Every other device is signed out.</p>}
      <div>
        <button type="submit" disabled={current === "" || next === "" || change.isPending}
          className="min-h-11 rounded-lg bg-accent px-4 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 lg:min-h-0">
          Change password
        </button>
      </div>
    </form>
  );
}
