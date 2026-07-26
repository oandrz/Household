import { InviteScreen } from "./features/auth/InviteScreen";
import { SignInScreen } from "./features/auth/SignInScreen";
import { useMe } from "./features/auth/useAuth";

// The invite-accept flow is reached from an emailed link
// (`${BaseURL}/invite/${token}`, minted in usecase/invite.go) and is public --
// it must render before there is any session to check. A plain pathname
// match is enough here: standing up @tanstack/react-router is Task 19's
// concern (the app shell), not this one's.
function inviteTokenFromPath(pathname: string): string | null {
  const match = /^\/invite\/([^/]+)\/?$/.exec(pathname);
  return match ? decodeURIComponent(match[1]) : null;
}

export function App() {
  const inviteToken = inviteTokenFromPath(window.location.pathname);
  if (inviteToken) {
    return <InviteScreen token={inviteToken} />;
  }
  return <SignedInOrSignIn />;
}

// A deliberately minimal shell for the signed-in state. Task 19 replaces
// this with the real sidebar and dashboard; its only job today is to prove
// that a live session renders something other than the sign-in screen.
function SignedInOrSignIn() {
  const me = useMe();

  if (me.isPending) {
    return (
      <main className="min-h-screen grid place-items-center p-10">
        <p className="text-muted text-sm">Loading…</p>
      </main>
    );
  }

  if (me.isError) {
    return <SignInScreen />;
  }

  return (
    <main className="min-h-screen grid place-items-center p-10">
      <div className="bg-card border border-hairline rounded-[8px] shadow-[var(--shadow-card)] p-8 max-w-md">
        <h1 className="font-serif text-2xl mb-2">Hearth</h1>
        <p className="text-muted text-sm">
          Signed in as {me.data.user.displayName}. The real app shell arrives
          in the next task.
        </p>
      </div>
    </main>
  );
}
