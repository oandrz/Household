// Step 2 of the design's "Create household" state (design/Household
// Dashboard.dc.html, the authCreate branch): household name, primary
// currency, display name and password, collected once the mailed link has
// proven the address. See SignUpScreen.tsx (step 1) for why the address is
// collected first, on a separate screen, before any of this.
//
// The design's own authCreate block has no currency field (Task 22 is what
// introduced a currency choice to this codebase at all) -- everything else
// here (the card shell, the field markup) is taken verbatim from
// SignUpScreen.tsx and SignInScreen.tsx respectively, so all three auth
// cards stay visually identical.
import { type FormEvent, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "./copy";
import { type Currency } from "./schemas";
import { useCompleteSignUp, useCurrencies, useSignUpPreview } from "./useAuth";

function currencyLabel(c: Currency) {
  return c.symbol ? `${c.code} (${c.symbol}) — ${c.name}` : `${c.code} — ${c.name}`;
}

// Branches on the ApiError code, never on the server's message. InviteScreen
// does the same, for the reason its own comment gives: the message is copy the
// backend owns and may reword, while the code is the contract.
function SignUpTokenError({ error }: { error: unknown }) {
  const code = error instanceof ApiError ? error.code : null;

  let message: string;
  let action: { to: string; label: string };
  switch (code) {
    case "SIGNUP_ALREADY_USED":
      message = "This link has already been used.";
      action = { to: "/sign-in", label: "Sign in" };
      break;
    case "TOKEN_EXPIRED":
    // A token that never existed and one that has lapsed need the same next
    // step, so they share a branch rather than telling the visitor which of
    // the two it was -- which would also confirm whether a given token was
    // ever issued. Falls through deliberately.
    case "NOT_FOUND":
      message = "This link has expired. Start again to get a new one.";
      action = { to: "/sign-up", label: "Create a household" };
      break;
    default:
      message = apiErrorMessage(error, "We couldn't open that link. Please try again.");
      action = { to: "/sign-up", label: "Create a household" };
  }

  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>
        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
            That link won't work.
          </h1>
          <p role="alert" className="mb-5 text-[13px] leading-relaxed text-muted">
            {message}
          </p>
          <Link
            to={action.to}
            className="block w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white"
          >
            {action.label}
          </Link>
        </div>
      </div>
    </main>
  );
}

function CompleteSignUpForm({ token, email }: { token: string; email: string }) {
  const [householdName, setHouseholdName] = useState("");
  const [currency, setCurrency] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  // unknown, not ApiError | null: an onError handler receives whatever the
  // mutation rejected with, which can be a network TypeError or a zod
  // ParseError, not only an ApiError -- matching SignUpScreen's and
  // SignInScreen's own `error` state for the same reason.
  const [error, setError] = useState<unknown>(null);
  const [validationError, setValidationError] = useState<string | null>(null);

  const currencies = useCurrencies();
  const completeSignUp = useCompleteSignUp();
  const navigate = useNavigate();

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (completeSignUp.isPending) return;

    const trimmedHouseholdName = householdName.trim();
    const trimmedDisplayName = displayName.trim();

    // The button is inside a <form>, so `required` on each field covers the
    // empty case in a real browser -- but jsdom aside, a currency of "" is
    // "required" only in the sense that the browser blocks *empty*, not that
    // it blocks a placeholder value indistinguishable from a real one, and a
    // password one character short of the backend's own floor must never
    // reach the API just to be told so.
    if (!trimmedHouseholdName) {
      setValidationError("Enter a household name.");
      return;
    }
    if (!currency) {
      setValidationError("Choose a primary currency.");
      return;
    }
    if (!trimmedDisplayName) {
      setValidationError("Enter your name.");
      return;
    }
    if (password.length < 12) {
      setValidationError("Password must be at least 12 characters.");
      return;
    }
    setValidationError(null);
    setError(null);

    completeSignUp.mutate(
      {
        token,
        householdName: trimmedHouseholdName,
        displayName: trimmedDisplayName,
        primaryCurrency: currency,
        password,
      },
      {
        // The cookies are already set and the me cache is seeded (see
        // useCompleteSignUp's onSuccess), so the app is ready the moment this
        // resolves.
        onSuccess: () => navigate({ to: "/", replace: true }),
        // Every field stays populated on a rejected submit -- the token
        // survives it, so the person must be able to correct one field
        // (a taken currency? an unmet password rule the client guard above
        // didn't anticipate?) and retry, rather than retype everything.
        onError: (err) => setError(err),
      },
    );
  }

  const allCurrencies = currencies.data?.currencies ?? [];
  // The split is the wire's own `symbol`, not a list kept here: currencySymbols
  // on the server is already the judgement about which currencies are worth
  // surfacing, and duplicating it would let the two drift. Order inside each
  // group is the server's.
  const common = allCurrencies.filter((c) => c.symbol);
  const rest = allCurrencies.filter((c) => !c.symbol);

  const inlineErrorMessage =
    validationError ??
    (error ? apiErrorMessage(error, "Something went wrong. Please try again.") : null);

  return (
    <>
      <h1 className="mt-0.5 mb-1 font-serif text-[27px] font-medium tracking-[-0.015em]">
        Start your household.
      </h1>
      <p className="mb-5 text-[13px] leading-relaxed text-muted">
        One household for the whole family. Set it up once, invite your partner, add the kids later.
      </p>

      <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="sign-up-household-name" className="text-xs font-semibold text-label">
            Household name
          </label>
          <input
            id="sign-up-household-name"
            type="text"
            required
            value={householdName}
            onChange={(event) => setHouseholdName(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
          <p className="text-[11px] text-muted">
            Shown at the bottom of the sidebar, beside your name. Change it any time.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="sign-up-currency" className="text-xs font-semibold text-label">
            Primary currency
          </label>
          <select
            id="sign-up-currency"
            required
            value={currency}
            onChange={(event) => setCurrency(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          >
            {/* No pre-selection. Defaulting to SGD would ship a
                wrong-currency first impression to everyone who did not
                notice the field, which is the reason the field exists. */}
            <option value="">Choose a currency</option>
            {common.length > 0 && (
              <optgroup label="Common">
                {common.map((c) => (
                  <option key={c.code} value={c.code}>
                    {currencyLabel(c)}
                  </option>
                ))}
              </optgroup>
            )}
            {rest.length > 0 && (
              <optgroup label="All currencies">
                {rest.map((c) => (
                  <option key={c.code} value={c.code}>
                    {currencyLabel(c)}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="sign-up-name" className="text-xs font-semibold text-label">
            Your name
          </label>
          <input
            id="sign-up-name"
            type="text"
            autoComplete="name"
            required
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="sign-up-email" className="text-xs font-semibold text-label">
            Email
          </label>
          <input
            id="sign-up-email"
            type="email"
            autoComplete="email"
            // Read-only: this is the address the mailed token proved. Letting
            // it be edited would mean the form could create an account for an
            // address nobody verified.
            readOnly
            value={email}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <div className="flex items-baseline justify-between">
            <label htmlFor="sign-up-password" className="text-xs font-semibold text-label">
              Password
            </label>
            {/* The design says "At least 10 characters"; the backend has
                always enforced 12 (and MapDomainError already answers
                "Password must be at least 12 characters" on a rejected
                request), so the hint here says what the backend actually
                requires rather than what the design happened to draw. */}
            <div className="text-[11.5px] text-muted">At least 12 characters</div>
          </div>
          <input
            id="sign-up-password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
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
          disabled={completeSignUp.isPending}
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
    </>
  );
}

export function SignUpCompleteScreen({ token }: { token: string }) {
  const preview = useSignUpPreview(token);

  if (preview.isError) {
    return <SignUpTokenError error={preview.error} />;
  }

  return (
    <main className="min-h-dvh grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] shadow-[var(--shadow-auth-card)]">
          {/* A quiet loading line, not a blank card, while the preview is in
              flight -- a blank screen reads as a broken link. */}
          {preview.isPending && (
            <p className="text-[13px] leading-relaxed text-muted">Loading…</p>
          )}
          {preview.isSuccess && (
            <CompleteSignUpForm token={token} email={preview.data.email} />
          )}
        </div>

        <p className="max-w-[428px] text-center text-xs leading-relaxed text-muted">
          You can invite your partner right after — nothing is shared until they accept.
          <br />
          <span className="text-[11px]">
            Your household data stays inside your household.
          </span>
        </p>
      </div>
    </main>
  );
}
