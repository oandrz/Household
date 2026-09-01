import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AdminGate } from "./AdminGate";
import { ApiError } from "../../api/client";

function renderGate(
  error: ApiError | null,
  options: { onSubmit?: (password: string) => void; pending?: boolean } = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminGate error={error} onSubmit={options.onSubmit ?? vi.fn()} pending={options.pending}>
        <div>the admin surface</div>
      </AdminGate>
    </QueryClientProvider>,
  );
}

describe("AdminGate", () => {
  it("renders the surface when nothing has failed", async () => {
    renderGate(null);
    await waitFor(() => expect(screen.getByText("the admin surface")).toBeInTheDocument());
  });

  // The distinct code is the whole reason the server sends one: a password
  // prompt, not a bounce to sign-in.
  it("asks for the password on ADMIN_REAUTH_REQUIRED", async () => {
    renderGate(new ApiError(401, "ADMIN_REAUTH_REQUIRED", "Confirm your password."));
    await waitFor(() => expect(screen.getByLabelText(/password/i)).toBeInTheDocument());
    expect(screen.queryByText("the admin surface")).not.toBeInTheDocument();
  });

  // A non-admin must see exactly what any other wrong URL gives them.
  it("renders not-found on a 404", async () => {
    renderGate(new ApiError(404, "NOT_FOUND", "That endpoint does not exist."));
    await waitFor(() => expect(screen.getByText(/not found/i)).toBeInTheDocument());
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it("says so while the admin surface is locked", async () => {
    renderGate(new ApiError(423, "ADMIN_LOCKED", "Too many failed attempts."));
    await waitFor(() => expect(screen.getByText(/too many failed attempts/i)).toBeInTheDocument());
  });

  // Review round 1, Finding 2: a wrong password on the re-authentication
  // form is a 401 too (INVALID_CREDENTIALS, admin_reauth.go's Verify), but
  // it must never fall into the fail-closed default below -- that would
  // turn a plain typo into a fake 404.
  it("shows the wrong-password prompt, not not-found, on INVALID_CREDENTIALS", async () => {
    renderGate(new ApiError(401, "INVALID_CREDENTIALS", "That password is incorrect."));
    await waitFor(() => expect(screen.getByLabelText(/password/i)).toBeInTheDocument());
    expect(screen.getByText("That password is incorrect.")).toBeInTheDocument();
    expect(screen.queryByText("the admin surface")).not.toBeInTheDocument();
    expect(screen.queryByText(/not found/i)).not.toBeInTheDocument();
  });

  // Review round 1, Finding 2: the fail-closed default must actually refuse
  // a code nobody has taught this file, not just the one named NOT_FOUND --
  // FORBIDDEN (403) is real (errors.go's own domain.ErrForbidden mapping)
  // but AdminGate has never been told what it means.
  it("renders not-found, never the children, on a code AdminGate does not recognise", async () => {
    renderGate(new ApiError(403, "FORBIDDEN", "You do not have permission to do that."));
    await waitFor(() => expect(screen.getByText(/not found/i)).toBeInTheDocument());
    expect(screen.queryByText("the admin surface")).not.toBeInTheDocument();
  });

  // Review round 1, Finding 3: requirePlatformAdmin's own doc comment says a
  // lookup failure is a 500, not the 404 it otherwise gives a non-admin,
  // specifically so an outage never reads as "you are not an admin" --
  // NotFoundScreen would throw that distinction away.
  it("shows the server's own message for a 5xx failure instead of not-found", async () => {
    renderGate(
      new ApiError(503, "AUDIT_UNAVAILABLE", "The admin surface is closed because its audit log cannot be written."),
    );
    await waitFor(() =>
      expect(
        screen.getByText("The admin surface is closed because its audit log cannot be written."),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByText(/not found/i)).not.toBeInTheDocument();
    expect(screen.queryByText("the admin surface")).not.toBeInTheDocument();
  });

  // Review round 1, Finding 3: a network failure never reaches the server
  // at all, so it cannot carry a code -- useAdmin.ts's toAdminGateError
  // coerces it to status 0, which must land here too, not on NotFoundScreen.
  it("shows a message, not not-found, for a coerced non-ApiError failure", async () => {
    renderGate(new ApiError(0, "UNKNOWN", "Something went wrong loading the admin surface."));
    await waitFor(() =>
      expect(screen.getByText("Something went wrong loading the admin surface.")).toBeInTheDocument(),
    );
    expect(screen.queryByText(/not found/i)).not.toBeInTheDocument();
  });

  // Review round 1, Finding 4: DefaultLockoutPolicy is three attempts, and a
  // failure recorded while a submission is already in flight (an impatient
  // double-click on a slow network) still counts against it -- the button
  // must not be clickable a second time before the first answer lands.
  it("disables the submit button while a submission is pending", async () => {
    renderGate(new ApiError(401, "ADMIN_REAUTH_REQUIRED", "Confirm your password."), { pending: true });
    await waitFor(() => expect(screen.getByRole("button", { name: /confirming/i })).toBeDisabled());
  });

  // Review round 1, Finding 9: the lock expires on its own in
  // DefaultLockoutPolicy's window -- "Try again" is the free door back,
  // adminctl unlock-admin (named on the same screen) is the faster one that
  // needs shell access, not the only one.
  it("lets the operator try again after a lockout, without needing adminctl", async () => {
    const onSubmit = vi.fn();
    renderGate(new ApiError(423, "ADMIN_LOCKED", "Too many failed attempts."), { onSubmit });

    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));

    const input = await screen.findByLabelText(/password/i);
    fireEvent.change(input, { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(onSubmit).toHaveBeenCalledWith("correct horse battery staple");
  });
});
