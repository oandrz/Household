// The admin surface's single point of fail-closed decision-making. Every
// admin route mounts its real content as this component's `children`, and
// nothing else in this file may render them: the moment a caller other than
// the `error === null` branch below returns `children`, a non-admin or a
// lapsed grant sees the real screen instead of a refusal.
//
// `error` is deliberately the caller's whole state, not something this
// component fetches for itself -- AdminShell owns the request(s) that can
// produce ADMIN_REAUTH_REQUIRED, NOT_FOUND or ADMIN_LOCKED, because the
// three codes come from more than one endpoint (see AdminShell's own
// comment on where each one arrives from). This file only has to know what
// each code means once it exists.
import { type FormEvent, useState } from "react";
import { ApiError } from "../../api/client";
import { NotFoundScreen } from "../shell/NotFoundScreen";

// Full-screen states share this shell (a centered card on the app's
// min-h-dvh pattern) so the password prompt and the lockout message read as
// one family, distinct from NotFoundScreen's plain centered line -- that
// contrast is itself part of "not found" reading as genuinely different
// from "found, but closed".
function AdminScreen({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <main className="grid min-h-dvh place-items-center bg-canvas p-6">
      <div className="w-full max-w-[380px] rounded-2xl border border-hairline bg-card px-7 py-8 shadow-[var(--shadow-auth-card)]">
        <h1 className="mb-1 font-serif text-[22px] font-medium tracking-[-0.01em]">{title}</h1>
        {children}
      </div>
    </main>
  );
}

// ADMIN_REAUTH_REQUIRED and INVALID_CREDENTIALS share this one screen: both
// come from the exact same re-authentication attempt (see AdminShell),
// distinguished only by whether a password has been tried yet. Folding a
// wrong password into the generic fail-closed branch below would show a
// "not found" page for a plain typo, which is not what "fail closed" is
// for -- the surface stays exactly this closed either way, this is only
// which closed screen explains why.
function PasswordPrompt({
  wrongPassword,
  onSubmit,
}: {
  wrongPassword: boolean;
  onSubmit: (password: string) => void;
}) {
  const [password, setPassword] = useState("");

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onSubmit(password);
  }

  return (
    <AdminScreen title="Confirm your password">
      <p className="mb-5 text-[13px] leading-relaxed text-muted">
        Re-enter your password to open the admin surface.
      </p>
      <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="admin-reauth-password" className="text-xs font-semibold text-label">
            Password
          </label>
          <input
            id="admin-reauth-password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className={
              wrongPassword
                ? "rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[13.5px]"
                : "rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            }
          />
          {wrongPassword && (
            <div role="alert" className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger">
              <span className="font-bold">!</span>
              <span>That password is incorrect.</span>
            </div>
          )}
        </div>
        <button
          type="submit"
          className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white"
        >
          Continue
        </button>
      </form>
    </AdminScreen>
  );
}

function LockoutMessage({ message }: { message: string }) {
  return (
    <AdminScreen title="Admin surface locked">
      <p className="text-[13px] leading-relaxed text-muted">{message}</p>
      <p className="mt-3 text-[13px] leading-relaxed text-muted">
        Run <code className="rounded bg-canvas px-1 py-0.5 font-mono text-[12px]">adminctl unlock-admin</code>{" "}
        on the box to open it again.
      </p>
    </AdminScreen>
  );
}

export function AdminGate({
  error,
  onSubmit,
  children,
}: {
  error: ApiError | null;
  onSubmit: (password: string) => void;
  children: React.ReactNode;
}) {
  if (error === null) return <>{children}</>;

  switch (error.code) {
    case "ADMIN_REAUTH_REQUIRED":
      return <PasswordPrompt wrongPassword={false} onSubmit={onSubmit} />;
    // A wrong password on the same re-authentication form the line above
    // renders -- see this file's header comment on PasswordPrompt.
    case "INVALID_CREDENTIALS":
      return <PasswordPrompt wrongPassword onSubmit={onSubmit} />;
    case "ADMIN_LOCKED":
      return <LockoutMessage message={error.message} />;
    default:
      // NOT_FOUND lands here, and so does anything this file has never
      // heard of -- both must be indistinguishable from a URL that was
      // never routed at all, which is exactly what NotFoundScreen already
      // is for rootRoute's own 404. Never widen this default to a case
      // that returns `children`.
      return <NotFoundScreen />;
  }
}
