// Shown after POST /auth/magic-link answers 202. That answer is unconditional
// -- it comes back whether or not the address has an account, and the send
// itself is fire-and-forget, so a mail relay failure is invisible by design
// (see usecase/auth.go's RequestMagicLink doc comment). Nothing will ever
// prompt a retry on the caller's behalf, so this panel has to: say the link
// lasts 15 minutes, suggest checking spam, and offer to send another. The
// design document has no equivalent screen for this state; the copy below is
// original, written in the sign-in screen's voice.
//
// The markup itself lives in CheckYourEmailPanel, shared with SignUpScreen's
// own "sent" state -- this is now just that panel's copy for the magic-link
// case.
import { CheckYourEmailPanel } from "./CheckYourEmailPanel";

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
    <CheckYourEmailPanel
      heading="Check your email."
      body={`If ${email || "that address"} has an account, we've sent a one-time sign-in link. It's good for the next 15 minutes.`}
      pending={pending}
      error={error}
      resendLabel="Send another link"
      pendingResendLabel="Sending…"
      onResend={onResend}
      backLabel="Use a password instead"
      onBack={onBack}
    />
  );
}
