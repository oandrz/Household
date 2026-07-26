// The invite-acceptance screen: GET /invites/:token, then a password (plus
// display name) form that posts to /invites/:token/accept.
//
// Copy: the "{inviter} invited you in." line and the "Joining as ..." banner
// come from the design document's Invited state (turn 5's #5a card,
// authInvite branch). The shared-spaces sentence there
// ("You'll share Money, Marriage and Family — everything except her private
// retro notes.") is written for one specific persona (an owner invited by
// Christine, with a gendered pronoun and a feature -- private retro notes --
// that isn't part of the capability model this screen actually has data
// for); this renders the same list-of-spaces opening ("You'll share Money,
// Marriage and Family.") derived from the real capabilities, and drops the
// persona-specific tail. The footer's "Not you? Ask {inviter} to resend the
// invite." is genuine design copy, now with the real inviter name.
import { type FormEvent, type ReactNode, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../../api/client";
import {
  apiErrorMessage,
  formatList,
  limitedAccessPhrase,
  roleLabel,
  sharedSpaceNames,
} from "./copy";
import { invitePreviewSchema, type InvitePreview } from "./schemas";
import { useAcceptInvite } from "./useAuth";

async function fetchInvitePreview(token: string): Promise<InvitePreview> {
  const body = await apiFetch<unknown>(`/api/v1/invites/${encodeURIComponent(token)}`);
  return invitePreviewSchema.parse(body);
}

function AuthShell({ children }: { children: ReactNode }) {
  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>
        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] shadow-[var(--shadow-auth-card)]">
          {children}
        </div>
      </div>
    </main>
  );
}

function InvitePreviewError({ error }: { error: unknown }) {
  let message =
    "Something went wrong loading this invite. Please try again.";
  if (error instanceof ApiError) {
    if (error.status === 404) {
      message =
        "We couldn't find that invite. Check the link, or ask whoever invited you to send a new one.";
    } else if (error.status === 410) {
      message =
        "This invite has expired. Ask whoever invited you to send a new one.";
    } else if (error.status === 409) {
      return (
        <p role="alert" className="text-[13px] leading-relaxed text-muted">
          This invite has already been accepted.{" "}
          <a href="/" className="font-medium text-accent">
            Sign in
          </a>{" "}
          instead.
        </p>
      );
    }
  }
  return (
    <p role="alert" className="text-[13px] leading-relaxed text-muted">
      {message}
    </p>
  );
}

function AcceptInviteForm({
  token,
  preview,
}: {
  token: string;
  preview: InvitePreview;
}) {
  const [displayName, setDisplayName] = useState(preview.name);
  const [password, setPassword] = useState("");
  const acceptInvite = useAcceptInvite();

  const spaces = formatList(sharedSpaceNames(preview.role, preview.capabilities));
  const label = roleLabel(preview.role);
  const bannerDetail =
    preview.role === "owner"
      ? "full access, equal say on every agreement"
      : `${limitedAccessPhrase(preview.capabilities)} access only`;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    acceptInvite.mutate({ token, password, displayName });
  }

  return (
    <>
      {/* The design places a household caption directly above this heading
          (turn 5's #5a card, authInvite branch, the avatar-row line above
          "Christine invited you in."). That line also names a member count
          ("2 adults, 2 kids") the invite preview response has no field for
          -- householdName is what's actually available, so that's all this
          renders. */}
      <p className="mb-1 text-[12.5px] font-medium text-muted">
        {preview.householdName}
      </p>
      <h1 className="mt-0.5 mb-1 font-serif text-[27px] font-medium tracking-[-0.015em]">
        {preview.inviterName} invited you in.
      </h1>
      {spaces && (
        <p className="mb-4 text-[13px] leading-relaxed text-muted">
          You'll share {spaces}.
        </p>
      )}
      <div className="mb-5 rounded-[10px] border border-callout-border bg-callout px-3.5 py-3 text-[12.5px] leading-relaxed text-accent">
        Joining as <strong className="font-semibold">{label}</strong> &mdash;{" "}
        {bannerDetail}.
      </div>

      <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="invite-name" className="text-xs font-semibold text-label">
            Name
          </label>
          <input
            id="invite-name"
            type="text"
            autoComplete="name"
            required
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="invite-password" className="text-xs font-semibold text-label">
            Password
          </label>
          <input
            id="invite-password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
          <p className="text-[11px] text-muted">At least 12 characters.</p>
          {acceptInvite.isError && (
            <div
              role="alert"
              className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger"
            >
              <span className="font-bold">!</span>
              <span>
                {apiErrorMessage(
                  acceptInvite.error,
                  "Something went wrong. Please try again.",
                )}
              </span>
            </div>
          )}
        </div>

        <button
          type="submit"
          disabled={acceptInvite.isPending}
          className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
        >
          Accept &amp; join household
        </button>
      </form>

      <p className="mt-4 text-center text-xs leading-relaxed text-muted">
        Not you? Ask {preview.inviterName} to resend the invite.
      </p>
    </>
  );
}

export function InviteScreen({ token }: { token: string }) {
  const preview = useQuery({
    queryKey: ["invite", token],
    queryFn: () => fetchInvitePreview(token),
    retry: false,
  });

  return (
    <AuthShell>
      {preview.isPending && (
        <p className="text-[13px] leading-relaxed text-muted">Loading your invite…</p>
      )}
      {preview.isError && <InvitePreviewError error={preview.error} />}
      {preview.isSuccess && (
        <AcceptInviteForm token={token} preview={preview.data} />
      )}
    </AuthShell>
  );
}
