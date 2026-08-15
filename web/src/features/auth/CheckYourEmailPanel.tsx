// The shared "we've sent you something" card. MagicLinkSentPanel and
// SignUpScreen's sent state are both callers; the markup lives here once so the
// two cannot drift apart visually.
//
// The design document has no equivalent screen for this state, so the copy is
// original, written in the sign-in screen's voice.
export function CheckYourEmailPanel({
  heading,
  body,
  pending,
  error,
  resendLabel,
  pendingResendLabel,
  onResend,
  backLabel,
  onBack,
}: {
  heading: string;
  body: string;
  pending: boolean;
  // A resend calls the same endpoint as the original send; a failure (429, 500,
  // a network rejection) must not look identical to success, because this panel
  // is the only thing on screen.
  error: string | null;
  resendLabel: string;
  pendingResendLabel: string;
  onResend: () => void;
  backLabel: string;
  onBack: () => void;
}) {
  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
            {heading}
          </h1>
          <p className="mb-1 text-[13px] leading-relaxed text-muted">{body}</p>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            Don't see it? Check spam, or send another.
          </p>

          <button
            type="button"
            onClick={onResend}
            disabled={pending}
            className="w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {pending ? pendingResendLabel : resendLabel}
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
            {backLabel}
          </button>
        </div>
      </div>
    </main>
  );
}
