// Reached from the emailed magic-link URL
// (`${BaseURL}/sign-in/magic?token=...`, minted in usecase/auth.go's
// RequestMagicLink). The design has no screen for this state at all -- its
// magic-link coverage stops at the "sent" panel -- so the copy below is
// authored, in the sign-in screen's established voice, not transcribed.
//
// Opening the link does nothing until the person clicks "Continue signing
// in". This is a security decision, not a UX flourish -- do not "improve" it
// back into consume-on-mount:
//
//   - Login CSRF. Anyone can request a magic link for their *own* account and
//     send the URL to someone else. If opening it signed the visitor in by
//     itself, the victim would be silently switched into the attacker's
//     household and type their real balances into it. The click, plus the
//     "Signed in as ..." warning below, is what gives them the chance to
//     notice.
//   - Link scanners. Mail filters that pre-open links in a headless browser
//     would otherwise spend the single-use token before the person ever
//     clicked it.
//
// The token is single-use (ConsumeMagicLink), so the click must fire the
// consume request exactly once: the button is disabled while the request is
// in flight and firedRef guards a double click that lands before the
// disabled state renders.
//
// Both outcomes (sign-in success -> navigate; failure -> show the message)
// are handled inside useConsumeMagicLink itself, not here -- see that
// hook's comment in useAuth.ts for why.
import { useRef } from "react";
import { useConsumeMagicLink, useMe } from "./useAuth";

// The same warning, in the same words, as InviteScreen's: a shared device
// with someone already signed in is exactly the situation login CSRF relies
// on, so it is said out loud before the click rather than discovered after.
function ExistingSessionWarning({ displayName }: { displayName: string }) {
  return (
    <p
      role="status"
      className="mb-4 rounded-lg border border-hairline bg-canvas px-3.5 py-3 text-left text-[12.5px] leading-relaxed text-label"
    >
      Signed in as <strong className="font-semibold">{displayName}</strong>.
      Continuing will sign them out and sign you in with this link instead.
    </p>
  );
}

export function MagicLinkConsumeScreen({ token }: { token: string }) {
  const { mutate, isPending, errorMessage } = useConsumeMagicLink();
  const me = useMe();
  const firedRef = useRef(false);

  function continueSigningIn() {
    if (firedRef.current) return;
    firedRef.current = true;
    mutate({ token });
  }

  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      {/* w-full: main's `place-items-center` leaves this wrapper shrink-to-fit,
          so the card's `max-w-[428px]` below has no definite containing block
          to resolve its `w-full` against -- without this, the card silently
          shrinks to its content's own width (measured 280px) at 1440.

          min-w-0: this box is a grid item, so its `min-width` is `auto` --
          its own min-content width -- and that floors the auto-sized track
          it sits in. Anything inside with a wide min-content therefore
          widens the whole page rather than being made to fit: one long
          <option> in the sign-up screen's currency <select> ("BAM -- Bosnia
          and Herzegovina convertible mark") held the card at its full 428px
          on a 375px phone, and the page scrolled sideways. Zero lets the
          track shrink to the viewport instead. */}
      <div className="w-full min-w-0 flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-full max-w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          {errorMessage ? (
            <>
              <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
                That link didn't work.
              </h1>
              <p role="alert" className="mb-5 text-[13px] leading-relaxed text-danger">
                {errorMessage}
              </p>
              <a
                href="/sign-in"
                className="text-[12.5px] font-medium text-accent"
              >
                Back to sign in
              </a>
            </>
          ) : (
            <>
              {me.isSuccess && (
                <ExistingSessionWarning displayName={me.data.user.displayName} />
              )}
              <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
                Sign in to Hearth
              </h1>
              <p className="mb-5 text-[13px] leading-relaxed text-muted">
                You opened a sign-in link. Continue only if you asked for it.
              </p>
              <button
                type="button"
                onClick={continueSigningIn}
                disabled={isPending}
                className="w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
              >
                {isPending ? "Signing you in…" : "Continue signing in"}
              </button>
            </>
          )}
        </div>
      </div>
    </main>
  );
}
