// Settings' "Your sign-in" card: change your own password, or sign out
// everywhere. Its own card, not part of Access: an owner sees every member's
// rows there, so a button in it would read as household-wide, and Access is
// "every way in besides a password".
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { useMe, useSignOutEverywhere } from "../auth/useAuth";
import { ChangePasswordForm } from "./ChangePasswordForm";

const SIGN_OUT_EVERYWHERE_WITH_EMAIL =
  "Signs you out on every device, including this one. Your API tokens are deleted and your Telegram chat is disconnected, so scripts and the bot stop working until you set them up again.";
const SIGN_OUT_EVERYWHERE_TELEGRAM_ONLY =
  "Signs you out on every device, including this one. Your API tokens are deleted. Your Telegram chat stays connected, because it is how you sign in.";

export function YourSignInPanel() {
  const me = useMe();
  const signOutEverywhere = useSignOutEverywhere();
  const [confirming, setConfirming] = useState(false);
  // A Telegram-only account has no email: its password signs nothing in, and
  // its chat is kept by the server (it is the only way back in).
  const hasEmail = (me.data?.user.email ?? "") !== "";

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-1 text-sm font-semibold text-ink">Your sign-in</h2>
      <p className="mb-4 text-xs text-muted">Only you. Nothing here changes anyone else's access.</p>
      {me.isSuccess && (
        <div className="flex flex-col gap-5">
          {hasEmail && <ChangePasswordForm />}
          <div>
            <button type="button" onClick={() => setConfirming(true)}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[13px] font-semibold text-danger lg:min-h-0">
              Sign out everywhere
            </button>
          </div>
        </div>
      )}
      <Modal open={confirming} onClose={() => setConfirming(false)} title="Sign out everywhere?">
        <div className="flex flex-col gap-3 text-[13px]">
          <p className="text-ink">{hasEmail ? SIGN_OUT_EVERYWHERE_WITH_EMAIL : SIGN_OUT_EVERYWHERE_TELEGRAM_ONLY}</p>
          {signOutEverywhere.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(signOutEverywhere.error, "Nothing was signed out. Try again.")}
            </p>
          )}
          <ModalActions
            secondaryLabel="Cancel"
            onSecondary={() => setConfirming(false)}
            primaryLabel="Sign out everywhere now"
            primaryType="button"
            onPrimary={() => signOutEverywhere.mutate()}
            primaryDisabled={signOutEverywhere.isPending}
          />
        </div>
      </Modal>
    </section>
  );
}
