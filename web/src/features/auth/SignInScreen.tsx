// The sign-in screen. Copy is taken verbatim from the design document's
// "Sign in" state (design/Household Dashboard.dc.html, turn 5's #5a card,
// authReturning branch) except where noted below.
//
// This is one component with a `mode` of "password" | "magic-sent", not four
// screens. The locked state is the password mode with an error and a
// disabled submit button -- the design shows one screen with varying copy,
// not a separate locked screen (there is no such state in the design's own
// authScreen enum, which only has Sign in / Invited / Wrong password /
// Signed in).
import { type FormEvent, useState } from "react";
import { ApiError } from "../../api/client";
import { triesLeftPhrase } from "./copy";
import { MagicLinkSentPanel } from "./MagicLinkSentPanel";
import { useRequestMagicLink, useSignIn } from "./useAuth";

type Mode = "password" | "magic-sent";

export function SignInScreen() {
  const [mode, setMode] = useState<Mode>("password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<ApiError | null>(null);

  const signIn = useSignIn();
  const requestMagicLink = useRequestMagicLink();

  const locked = error?.code === "HOUSEHOLD_LOCKED";

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    signIn.mutate(
      { email, password },
      {
        onError: (err) => setError(err instanceof ApiError ? err : null),
      },
    );
  }

  // Also used by "Forgot?": magic link is the recovery path (it works even
  // while the household is locked -- see usecase/auth.go), so there is no
  // separate password-reset flow to wire this to.
  function handleMagicLink() {
    setError(null);
    requestMagicLink.mutate(
      { email },
      { onSuccess: () => setMode("magic-sent") },
    );
  }

  if (mode === "magic-sent") {
    return (
      <MagicLinkSentPanel
        email={email}
        pending={requestMagicLink.isPending}
        onResend={handleMagicLink}
        onBack={() => setMode("password")}
      />
    );
  }

  let errorMessage: string | null = null;
  if (locked) {
    // The design has no locked-screen copy of its own (its authScreen enum
    // stops at "Wrong password"); this reuses its established voice and the
    // "15 minutes" figure from the wrong-password line, and -- per the
    // recovery-path requirement -- points at the magic-link option below,
    // which stays enabled.
    errorMessage =
      "This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.";
  } else if (error?.code === "INVALID_CREDENTIALS") {
    const attemptsRemaining = Number(error.details.attemptsRemaining ?? 0);
    errorMessage = `That password doesn't match. ${triesLeftPhrase(attemptsRemaining)} before we lock the household for 15 minutes.`;
  } else if (error) {
    errorMessage = error.message;
  }

  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] shadow-[var(--shadow-auth-card)]">
          <h1 className="mt-0.5 mb-1 font-serif text-[27px] font-medium tracking-[-0.015em]">
            Welcome back.
          </h1>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            Sign in to pick up where you both left off.
          </p>

          <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
            <div className="flex flex-col gap-1.5">
              <label
                htmlFor="sign-in-email"
                className="text-xs font-semibold text-label"
              >
                Email
              </label>
              <input
                id="sign-in-email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <div className="flex items-baseline justify-between">
                <label
                  htmlFor="sign-in-password"
                  className="text-xs font-semibold text-label"
                >
                  Password
                </label>
                <button
                  type="button"
                  onClick={handleMagicLink}
                  className="cursor-pointer text-[11.5px] font-medium text-accent"
                >
                  Forgot?
                </button>
              </div>
              <input
                id="sign-in-password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                className={
                  error
                    ? "rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[13.5px]"
                    : "rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
                }
              />
              {errorMessage && (
                <div className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger">
                  <span className="font-bold">!</span>
                  <span>{errorMessage}</span>
                </div>
              )}
            </div>

            <button
              type="submit"
              disabled={locked || signIn.isPending}
              className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
            >
              Continue
            </button>
          </form>

          <div className="my-4 flex items-center gap-3">
            <div className="h-px flex-1 bg-hairline" />
            <div className="text-[11px] uppercase tracking-[0.08em] text-muted">or</div>
            <div className="h-px flex-1 bg-hairline" />
          </div>

          <button
            type="button"
            onClick={handleMagicLink}
            disabled={requestMagicLink.isPending}
            className="w-full rounded-[9px] border border-hairline py-2.5 text-center text-[13px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60"
          >
            Email me a one-time sign-in link
          </button>
        </div>

        <p className="text-center text-xs leading-relaxed text-muted">
          Your household data stays between the two of you.
        </p>
      </div>
    </main>
  );
}
