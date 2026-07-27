// Step 1 of the design's "Create household" state (design/Household
// Dashboard.dc.html, the authCreate branch): the email address only.
//
// The design draws one card that creates the household on submit. This is
// deliberately split in two, because collecting the household name and display
// name before the address is verified would let someone submit a sign-up for
// another person's address with a household name and a display name of their
// choosing -- and the mail would then invite that person into a household a
// stranger had configured. The person who clicks the link supplies their own
// details, on SignUpCompleteScreen.
//
// Forgot?, the "or" divider and the magic-link button are all absent because
// the design gates them on authNotCreate.
import { type FormEvent, useState } from "react";
import { Link } from "@tanstack/react-router";
import { apiErrorMessage, isPlausibleEmail } from "./copy";
import { CheckYourEmailPanel } from "./CheckYourEmailPanel";
import { useRequestSignUp } from "./useAuth";

export function SignUpScreen() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  // unknown, not ApiError | null: an onError handler receives whatever the
  // mutation rejected with, which can be a network TypeError or a zod
  // ParseError, not only an ApiError.
  const [error, setError] = useState<unknown>(null);
  const [validationError, setValidationError] = useState<string | null>(null);

  const requestSignUp = useRequestSignUp();

  function submit() {
    if (requestSignUp.isPending) return;

    const trimmed = email.trim();
    // The button is inside a <form>, so `required` covers the empty case -- but
    // an obviously-malformed address must not reach the API either, or the sent
    // panel appears for a request that could never deliver.
    if (!isPlausibleEmail(trimmed)) {
      setValidationError("Enter your email address to create a household.");
      return;
    }
    setValidationError(null);
    setError(null);
    requestSignUp.mutate(
      { email: trimmed },
      {
        onSuccess: () => setSent(true),
        onError: (err) => setError(err),
      },
    );
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submit();
  }

  // Resolved once, matching SignInScreen's own errorMessage pattern: `error`
  // is `unknown` (see its declaration above), and a JSX conditional built
  // directly from `validationError || error` widens to include `unknown`
  // wherever error is the truthy operand, which TypeScript then rejects as a
  // ReactNode. Resolving it to a string up front sidesteps that instead of
  // narrowing at each call site.
  const inlineErrorMessage =
    validationError ??
    (error ? apiErrorMessage(error, "That didn't go through. Please try again.") : null);

  if (sent) {
    return (
      <CheckYourEmailPanel
        heading="Check your email."
        // Describes both outcomes in one sentence. This panel cannot know which
        // mail was sent -- the API answers identically for a fresh address and a
        // registered one -- and must not appear to.
        body={`We've sent a link to ${email.trim() || "that address"}. If that address already has an account, we've sent sign-in instructions instead. Either way it's good for the next 24 hours.`}
        pending={requestSignUp.isPending}
        error={inlineErrorMessage}
        resendLabel="Send another link"
        pendingResendLabel="Sending…"
        onResend={submit}
        backLabel="Use a different address"
        onBack={() => setSent(false)}
      />
    );
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
            Start your household.
          </h1>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            One household, two owners. Set it up once and invite your partner in.
          </p>

          <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="sign-up-email" className="text-xs font-semibold text-label">
                Email
              </label>
              <input
                id="sign-up-email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(event) => {
                  setEmail(event.target.value);
                  if (validationError) setValidationError(null);
                  if (error) setError(null);
                }}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              />
              {inlineErrorMessage && (
                <div
                  role="alert"
                  className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger"
                >
                  <span className="font-bold">!</span>
                  <span>{inlineErrorMessage}</span>
                </div>
              )}
            </div>

            <button
              type="submit"
              disabled={requestSignUp.isPending}
              className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
            >
              Create household
            </button>
          </form>

          <div className="mt-[18px] border-t border-hairline pt-[15px] text-center text-[12.5px] text-muted">
            <span>Already set up? </span>
            <Link to="/sign-in" className="cursor-pointer font-semibold text-accent">
              Sign in
            </Link>
          </div>
        </div>

        <p className="max-w-[428px] text-center text-xs leading-relaxed text-muted">
          You can invite your partner right after — nothing is shared until they accept.
          <br />
          <span className="text-[11px]">
            Your household data stays between the two of you.
          </span>
        </p>
      </div>
    </main>
  );
}
