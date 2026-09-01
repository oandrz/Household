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
import { type FormEvent, useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "../../api/client";
import {
  apiErrorMessage,
  isPlausibleEmail,
  TELEGRAM_FALLBACK_ERROR,
  TELEGRAM_POPUP_BLOCKED_MESSAGE,
  triesLeftPhrase,
} from "./copy";
import { MagicLinkSentPanel } from "./MagicLinkSentPanel";
import { useRequestMagicLink, useSignIn, useStartTelegramSignIn } from "./useAuth";

type Mode = "password" | "magic-sent";

export function SignInScreen() {
  const [mode, setMode] = useState<Mode>("password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  // unknown, not ApiError | null: an onError handler receives whatever the
  // mutation rejected with, which can be a network TypeError or a zod
  // ParseError, not only an ApiError. Both sign-in and magic-link need their
  // own state -- clicking "Email me a one-time sign-in link" (or "Forgot?")
  // must not erase a sign-in error that's still on screen (see the locked
  // case below), and a failed resend from the sent panel must not be
  // confused with a failed initial send.
  const [signInError, setSignInError] = useState<unknown>(null);
  const [magicLinkError, setMagicLinkError] = useState<unknown>(null);
  // Fix round 4, Finding 3: a client-side "that doesn't look like an email"
  // message, distinct from magicLinkError above -- it is never an ApiError
  // (the request that would have produced one never fires), so it can't be
  // folded into apiErrorMessage's fallback without losing its actual text.
  // Cleared alongside magicLinkError on every mode change, for the identical
  // reason (fix round 2, Finding 1).
  const [magicLinkValidationError, setMagicLinkValidationError] = useState<
    string | null
  >(null);
  // Once a 404 shows this install has no bot configured, the control stays
  // hidden for the rest of this screen's life -- there is nothing that would
  // make a retry succeed, so there is no path back to showing it again.
  const [telegramUnavailable, setTelegramUnavailable] = useState(false);
  // A string, not `unknown` like signInError/magicLinkError above: this
  // control's only two outcomes worth distinguishing are "hide the button"
  // (handled by telegramUnavailable) and "say something went wrong", so the
  // message is computed once in the mutation's own onError rather than
  // carried as a raw error for the render to re-derive.
  const [telegramError, setTelegramError] = useState<string | null>(null);
  // Set only when the synchronous window.open below returned null -- the
  // person has popups blocked. Distinct from telegramError: this isn't a
  // failure, the request succeeded, there's just no window to point at the
  // result, so this carries the URL itself rather than a message.
  const [telegramFallbackUrl, setTelegramFallbackUrl] = useState<
    string | null
  >(null);

  const signIn = useSignIn();
  const requestMagicLink = useRequestMagicLink();
  const startTelegram = useStartTelegramSignIn();

  // Fix round 2, Finding 1: a magic-link error belongs to the mode it was
  // raised in (a failed resend belongs to the sent panel), so it must not
  // survive a transition to a different mode -- clearing it only in onBack's
  // handler covered the one path that existed then, but not any future path
  // back to the password form. Keying this off `mode` itself, rather than a
  // specific handler, covers all of them.
  //
  // This alone is not enough (fix round 3, Finding 1): it only clears an
  // error that already exists at the moment `mode` changes. It does nothing
  // for a request that is still in flight when the user navigates away and
  // only settles afterwards -- "Send another link" has no pending guard on
  // "Use a password instead", so that interleaving is reachable. `modeRef`
  // gives onSuccess/onError a way to check, at settle time, whether the
  // request is still relevant to the mode it started in; see handleMagicLink.
  useEffect(() => {
    setMagicLinkError(null);
    setMagicLinkValidationError(null);
  }, [mode]);

  const modeRef = useRef(mode);
  useEffect(() => {
    modeRef.current = mode;
  }, [mode]);

  const apiSignInError = signInError instanceof ApiError ? signInError : null;
  const locked = apiSignInError?.code === "HOUSEHOLD_LOCKED";

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSignInError(null);
    signIn.mutate(
      { email, password },
      { onError: (err) => setSignInError(err) },
    );
  }

  // Fix round 4, Finding 2: signInError (and the locked state it drives,
  // including the disabled Continue button) used to clear only inside
  // handleSubmit -- which cannot run while Continue is disabled, so once the
  // household locked there was no way back to a usable form short of
  // reloading the page. Clearing it here, on every edit to either field,
  // means typing itself re-enables the form: the very next keystroke after
  // a lockout (or a plain wrong-password rejection) clears the stale error
  // rather than leaving it stuck until a fresh submit that can never happen.
  function handleEmailChange(value: string) {
    setEmail(value);
    if (signInError) setSignInError(null);
  }
  function handlePasswordChange(value: string) {
    setPassword(value);
    if (signInError) setSignInError(null);
  }

  // Also used by "Forgot?": magic link is the recovery path (it works even
  // while the household is locked -- see usecase/auth.go), so there is no
  // separate password-reset flow to wire this to. Deliberately does not
  // touch signInError: clicking this while the locked explanation is showing
  // must not erase it before the new request resolves.
  //
  // Fix round 4, Finding 3: neither "Forgot?" nor its sibling is inside the
  // <form> (both are type="button"), so neither ever honours the email
  // field's `required` attribute the way a real submit would -- clicking
  // either with an empty or obviously-not-an-email field used to post
  // straight to the API and render the "Check your email" panel for
  // nothing. This guards on both a pending request (matching the disabled
  // attribute both controls now carry -- see the JSX below) and a
  // plausibly-formed, non-empty address before ever calling mutate, so a
  // request is only ever made once whichever pending request was already in
  // flight, and only ever posts an address that could conceivably belong to
  // someone.
  function handleMagicLink() {
    if (requestMagicLink.isPending) return;

    const trimmedEmail = email.trim();
    if (!isPlausibleEmail(trimmedEmail)) {
      setMagicLinkValidationError(
        "Enter your email address to get a sign-in link.",
      );
      return;
    }
    setMagicLinkValidationError(null);

    // Captured now, not read later: this is the mode the request started
    // in. `onSuccess`/`onError` below compare it against `modeRef.current`
    // (the *latest* mode, which a plain closure over `mode` could never see)
    // at the moment the request actually settles. If the user has since
    // navigated away -- clicked "Use a password instead" while this exact
    // request was still in flight -- the settled result is scoped to a mode
    // that's no longer current, and is dropped rather than applied. This
    // removes the whole class of "an abandoned request's result renders
    // somewhere it no longer belongs," not just the one reported sequence.
    const startedInMode = mode;
    setMagicLinkError(null);
    requestMagicLink.mutate(
      { email: trimmedEmail },
      {
        onSuccess: () => {
          if (modeRef.current !== startedInMode) return;
          setMode("magic-sent");
        },
        onError: (err) => {
          if (modeRef.current !== startedInMode) return;
          setMagicLinkError(err);
        },
      },
    );
  }

  const magicLinkErrorMessage =
    magicLinkValidationError ??
    (magicLinkError
      ? apiErrorMessage(
          magicLinkError,
          "That didn't go through. Please try again.",
        )
      : null);

  if (mode === "magic-sent") {
    return (
      <MagicLinkSentPanel
        email={email}
        pending={requestMagicLink.isPending}
        error={magicLinkErrorMessage}
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
  } else if (apiSignInError?.code === "INVALID_CREDENTIALS") {
    const attemptsRemaining = Number(
      apiSignInError.details.attemptsRemaining ?? 0,
    );
    errorMessage = `That password doesn't match. ${triesLeftPhrase(attemptsRemaining)} before we lock the household for 15 minutes.`;
  } else if (signInError) {
    errorMessage = apiErrorMessage(
      signInError,
      "Something went wrong. Please try again.",
    );
  }

  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      {/* w-full: main's `place-items-center` leaves this wrapper shrink-to-fit,
          so the card's `max-w-[428px]` below has no definite containing block
          to resolve its `w-full` against -- without this, the card silently
          shrinks to its content's own width (measured 280px) at 1440. */}
      <div className="w-full flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-full max-w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] shadow-[var(--shadow-auth-card)]">
          <h1 className="mt-0.5 mb-1 font-serif text-[27px] font-medium tracking-[-0.015em]">
            Welcome back.
          </h1>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            Sign in to pick up where you left off.
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
                onChange={(event) => handleEmailChange(event.target.value)}
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
                  disabled={requestMagicLink.isPending}
                  className="cursor-pointer text-[11.5px] font-medium text-accent disabled:cursor-not-allowed disabled:opacity-60"
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
                onChange={(event) => handlePasswordChange(event.target.value)}
                className={
                  signInError
                    ? "rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[13.5px]"
                    : "rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
                }
              />
              {errorMessage && (
                <div
                  role="alert"
                  className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger"
                >
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
          {magicLinkErrorMessage && (
            <div
              role="alert"
              className="mt-2 flex items-start gap-1.5 text-xs leading-snug text-danger"
            >
              <span className="font-bold">!</span>
              <span>{magicLinkErrorMessage}</span>
            </div>
          )}

          {!telegramUnavailable && (
            <>
              <button
                type="button"
                onClick={() => {
                  // Fix round 1, Item 2 (CONTROLLER RULING R12): clear both
                  // per-attempt banners at the start of every click, not just
                  // on success -- otherwise a stale error (or a stale
                  // popup-blocked link) from a previous failed attempt keeps
                  // showing under a control that just worked. This file
                  // already carries two earlier instances of exactly this
                  // defect class for magicLinkError/magicLinkValidationError
                  // ("Fix round 2, Finding 1" and "Fix round 3, Finding 1");
                  // this would have been the third.
                  setTelegramError(null);
                  setTelegramFallbackUrl(null);
                  // Fix round 1, Item 1 (CONTROLLER RULING R13): opened here,
                  // synchronously inside the click handler, not in
                  // onSuccess below. WebKit gates window.open on the
                  // synchronous gesture call stack, and an awaited fetch
                  // reliably breaks that gate -- the textbook
                  // OAuth-popup-blocked-on-Safari failure. Chromium's
                  // transient-activation window is looser (so a fetch-then-
                  // open often works on a fast desktop) but degrades under
                  // real mobile latency. Opening a blank tab now, then
                  // pointing it at the real URL once the mutation resolves,
                  // keeps the open() call itself inside the activation
                  // window regardless of how long the request takes.
                  //
                  // No "noopener" third argument here: per spec,
                  // window.open(url, target, "noopener") returns null, which
                  // would leave nothing to navigate once the response
                  // arrives. Dropping it is fine -- the URL this tab is
                  // later pointed at is first-party and minted by this
                  // backend, not attacker-controlled, so the opener-hijack
                  // risk "noopener" guards against does not apply here.
                  const popup = window.open("", "_blank");
                  startTelegram.mutate(undefined, {
                    onSuccess: (data) => {
                      if (popup) {
                        popup.location.href = data.url;
                        return;
                      }
                      // The synchronous open above still came back null --
                      // the person has popups hard-blocked. That is exactly
                      // the failure this whole shape exists to avoid, so
                      // silence is not an option: surface the URL as a real
                      // link so the flow is still completable with one more
                      // tap.
                      setTelegramFallbackUrl(data.url);
                    },
                    // A 404 means this install has no bot configured (see
                    // handleTelegramStart's own comment: it answers exactly
                    // like an unrouted path, on purpose). Hide the control
                    // rather than showing an error the person can do
                    // nothing about. Either way, the blank tab opened above
                    // has nowhere useful to go, so close it rather than
                    // leaving a bare about:blank tab behind.
                    onError: (err) => {
                      popup?.close();
                      if (err instanceof ApiError && err.status === 404) {
                        setTelegramUnavailable(true);
                        return;
                      }
                      setTelegramError(
                        apiErrorMessage(err, TELEGRAM_FALLBACK_ERROR),
                      );
                    },
                  });
                }}
                disabled={startTelegram.isPending}
                className="mt-2 w-full rounded-[9px] border border-hairline py-2.5 text-center text-[13px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60"
              >
                Continue with Telegram
              </button>
              {telegramError && (
                <div
                  role="alert"
                  className="mt-2 flex items-start gap-1.5 text-xs leading-snug text-danger"
                >
                  <span className="font-bold">!</span>
                  <span>{telegramError}</span>
                </div>
              )}
              {telegramFallbackUrl && (
                <p className="mt-2 text-xs leading-snug text-label">
                  {TELEGRAM_POPUP_BLOCKED_MESSAGE}{" "}
                  <a
                    href={telegramFallbackUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-semibold text-accent"
                  >
                    Open Telegram
                  </a>
                </p>
              )}
            </>
          )}

          <div className="mt-[18px] border-t border-hairline pt-[15px] text-center text-[12.5px] text-muted">
            <span>No household yet? </span>
            <Link to="/sign-up" className="cursor-pointer font-semibold text-accent">
              Create one
            </Link>
          </div>
        </div>

        <p className="text-center text-xs leading-relaxed text-muted">
          Your household data stays inside your household.
        </p>
      </div>
    </main>
  );
}
