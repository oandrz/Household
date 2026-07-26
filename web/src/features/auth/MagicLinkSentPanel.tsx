// Shown after POST /auth/magic-link answers 202. That answer is unconditional
// -- it comes back whether or not the address has an account, and the send
// itself is fire-and-forget, so a mail relay failure is invisible by design
// (see usecase/auth.go's RequestMagicLink doc comment). Nothing will ever
// prompt a retry on the caller's behalf, so this panel has to: say the link
// lasts 15 minutes, suggest checking spam, and offer to send another. The
// design document has no equivalent screen for this state; the copy below is
// original, written in the sign-in screen's voice.
export function MagicLinkSentPanel({
  email,
  pending,
  error,
  onResend,
  onBack,
}: {
  email: string;
  pending: boolean;
  // A resend calls the exact same endpoint as the original send; a failure
  // (429, 500, a network rejection) must not look identical to success --
  // this panel is the only thing on screen once "sent" mode is entered, so
  // if it doesn't surface the failure, nothing else will.
  error: string | null;
  onResend: () => void;
  onBack: () => void;
}) {
  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
            Check your email.
          </h1>
          <p className="mb-1 text-[13px] leading-relaxed text-muted">
            If {email || "that address"} has an account, we've sent a
            one-time sign-in link. It's good for the next 15 minutes.
          </p>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            Don't see it? Check spam, or send another.
          </p>

          <button
            type="button"
            onClick={onResend}
            disabled={pending}
            className="w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {pending ? "Sending…" : "Send another link"}
          </button>
          {error && (
            <div
              role="alert"
              className="mt-2 flex items-start justify-center gap-1.5 text-xs leading-snug text-danger"
            >
              <span className="font-bold">!</span>
              <span>{error}</span>
            </div>
          )}
          <button
            type="button"
            onClick={onBack}
            className="mt-3 text-[12.5px] font-medium text-accent"
          >
            Use a password instead
          </button>
        </div>
      </div>
    </main>
  );
}
