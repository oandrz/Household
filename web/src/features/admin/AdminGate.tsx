// The admin surface's single point of fail-closed decision-making. Every
// admin route mounts its real content as this component's `children`, and
// nothing else in this file may render them: the moment a caller other than
// the `error === null` branch below returns `children`, a non-admin or a
// lapsed grant sees the real screen instead of a refusal.
//
// `error` is deliberately the caller's whole state, not something this
// component fetches for itself -- AdminShell owns the request(s) that can
// produce each of the codes handled below, because they come from more than
// one endpoint (see AdminShell's own comment on where each one arrives
// from). This file only has to know what each code means once it exists.
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

// ADMIN_REAUTH_REQUIRED, INVALID_CREDENTIALS, and a retried ADMIN_LOCKED
// (see AdminGate's own ADMIN_LOCKED case) all share this one screen: every
// one of them comes from the same re-authentication attempt (see
// AdminShell), distinguished only by `errorMessage` -- null the first time
// the grant lapses, the server's own message once a submission has actually
// failed. Folding a wrong password into the generic fail-closed default
// below would show a "not found" page for a plain typo, which is not what
// "fail closed" is for -- the surface stays exactly this closed either way,
// this only chooses which closed screen explains why.
function PasswordPrompt({
  errorMessage,
  pending,
  onSubmit,
}: {
  errorMessage: string | null;
  pending: boolean;
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
              errorMessage
                ? "rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[13.5px]"
                : "rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            }
          />
          {errorMessage && (
            <div role="alert" className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger">
              <span className="font-bold">!</span>
              <span>{errorMessage}</span>
            </div>
          )}
        </div>
        <button
          type="submit"
          disabled={pending}
          className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
        >
          {pending ? "Confirming…" : "Continue"}
        </button>
      </form>
    </AdminScreen>
  );
}

// DefaultLockoutPolicy is three attempts before this triggers, and every
// failure -- even one recorded while already locked -- extends it
// (admin_reauth.go's own Verify comment). The lock expires on its own; the
// "Try again" button is the door that costs nothing, adminctl unlock-admin
// is the door that's faster but needs shell access. Presented as an "or",
// not as the only way back -- an earlier version of this screen implied the
// second was the only door, directly under the server's own "Try again in a
// few minutes.", which read as contradicting itself.
function LockoutMessage({ message, onTryAgain }: { message: string; onTryAgain: () => void }) {
  return (
    <AdminScreen title="Admin surface locked">
      <p className="text-[13px] leading-relaxed text-muted">{message}</p>
      <p className="mt-3 text-[13px] leading-relaxed text-muted">
        It reopens on its own once the lock expires, or an operator with
        shell access can run{" "}
        <code className="rounded bg-canvas px-1 py-0.5 font-mono text-[12px]">adminctl unlock-admin</code>{" "}
        sooner.
      </p>
      <button
        type="button"
        onClick={onTryAgain}
        className="mt-4 w-full rounded-[9px] border border-hairline py-2.5 text-center text-[13px] font-semibold text-label"
      >
        Try again
      </button>
    </AdminScreen>
  );
}

// requirePlatformAdmin's own doc comment (middleware_admin.go) is explicit
// that a lookup failure answers 500, never the 404 it otherwise gives a
// non-admin, "because 'the database is down' must not read as a clean 'you
// are not an admin'". auditAdmin's 503 AUDIT_UNAVAILABLE carries its own
// bespoke message for the identical reason. Routing either through
// NotFoundScreen would throw away the one thing each was written to say --
// this is where their message actually reaches the operator instead. A
// coerced non-ApiError failure (useAdmin.ts's toAdminGateError, status 0)
// lands here too, with its own generic message, for the same reason: a
// network blip is not "you are not an admin" either.
function ServerErrorScreen({ message }: { message: string }) {
  return (
    <AdminScreen title="Something went wrong">
      <p className="text-[13px] leading-relaxed text-muted">{message}</p>
    </AdminScreen>
  );
}

export function AdminGate({
  error,
  onSubmit,
  pending = false,
  children,
}: {
  error: ApiError | null;
  onSubmit: (password: string) => void;
  pending?: boolean;
  children: React.ReactNode;
}) {
  // Local, not derived from `error`: `error.code` stays ADMIN_LOCKED across
  // a retry that fails again (the lock hasn't actually expired yet), so
  // this is the only thing that can tell "just locked" apart from "chose to
  // try again" -- without it, clicking "Try again" and having the attempt
  // fail again would silently snap back to the full lockout screen instead
  // of showing the form with today's message on it, the same shared-screen
  // treatment INVALID_CREDENTIALS already gets.
  const [tryingAgain, setTryingAgain] = useState(false);

  if (error === null) return <>{children}</>;

  switch (error.code) {
    case "ADMIN_REAUTH_REQUIRED":
      return <PasswordPrompt errorMessage={null} pending={pending} onSubmit={onSubmit} />;
    case "INVALID_CREDENTIALS":
      return <PasswordPrompt errorMessage={error.message} pending={pending} onSubmit={onSubmit} />;
    case "ADMIN_LOCKED":
      return tryingAgain ? (
        <PasswordPrompt errorMessage={error.message} pending={pending} onSubmit={onSubmit} />
      ) : (
        <LockoutMessage message={error.message} onTryAgain={() => setTryingAgain(true)} />
      );
    default:
      // A lookup failure and AUDIT_UNAVAILABLE both carry a message written
      // specifically so an outage doesn't read as a refusal -- see
      // ServerErrorScreen's own comment. NOT_FOUND, and anything else this
      // file has never heard of, still falls to NotFoundScreen below: both
      // must be indistinguishable from a URL that was never routed at all.
      // Never widen either branch to return `children`.
      if (error.status === 0 || error.status >= 500) {
        return <ServerErrorScreen message={error.message} />;
      }
      return <NotFoundScreen />;
  }
}
