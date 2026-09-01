import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AdminGate } from "./AdminGate";
import { ApiError } from "../../api/client";

function renderGate(error: ApiError | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminGate error={error} onSubmit={vi.fn()}>
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
});
