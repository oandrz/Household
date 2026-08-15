// Reached from the emailed magic-link URL
// (`${BaseURL}/sign-in/magic?token=...`, minted in usecase/auth.go's
// RequestMagicLink). The design has no screen for this state at all -- its
// magic-link coverage stops at the "sent" panel -- so the copy below is
// authored, in the sign-in screen's established voice, not transcribed.
//
// The token is single-use (ConsumeMagicLink), so this must fire the consume
// request exactly once even under StrictMode's double-invoke of effects --
// a second call for the same token would fail even though the first one
// already signed the user in, turning a successful sign-in into a visible
// error.
//
// Both outcomes (sign-in success -> navigate; failure -> show the message)
// are handled inside useConsumeMagicLink itself, not here -- see that
// hook's comment in useAuth.ts for why. This screen only fires the mutation
// once and renders whichever of "Signing you in…" or the error state the
// hook reports back.
import { useEffect, useRef } from "react";
import { useConsumeMagicLink } from "./useAuth";

export function MagicLinkConsumeScreen({ token }: { token: string }) {
  const { mutate, errorMessage } = useConsumeMagicLink();
  const firedRef = useRef(false);

  useEffect(() => {
    if (firedRef.current) return;
    firedRef.current = true;
    mutate({ token });
    // Deliberately fires once for this screen's lifetime, keyed on nothing
    // but mount: token is a prop of a screen the router only ever renders
    // once per emailed link, and re-running this for a changed `token`
    // isn't a case that occurs (the token comes from the URL that reached
    // this screen). mutate is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
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
            <p className="text-[13px] leading-relaxed text-muted">
              Signing you in…
            </p>
          )}
        </div>
      </div>
    </main>
  );
}
