import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { NewApiTokenModal } from "./NewApiTokenModal";

const SECRET = "hearth_ab12cd34THE-REST-OF-THE-SECRET";
const CREATED = { id: "t-1", name: "laptop", prefix: "ab12cd34", expiresAt: "2026-12-22T00:00:00Z", token: SECRET };
const EMPTY_ACCESS = { telegramEnabled: false, tokens: [], chats: [] };

function Harness() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        reopen
      </button>
      <NewApiTokenModal open={open} onClose={() => setOpen(false)} />
    </>
  );
}

function renderModal() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("NewApiTokenModal", () => {
  it("creates a token with the name and 90 days by default, then shows the secret", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "POST /api/v1/auth/tokens": {
        status: 201,
        body: CREATED,
        capture: (body) => {
          posted = body;
        },
      },
      "GET /api/v1/household/access": { status: 200, body: EMPTY_ACCESS },
    });
    renderModal();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByText(SECRET)).toBeInTheDocument();
    expect(posted).toEqual({ name: "laptop", expiresInDays: 90 });
  });

  it("never shows the secret again after closing", async () => {
    stubFetchRoutes({
      "POST /api/v1/auth/tokens": { status: 201, body: CREATED },
      "GET /api/v1/household/access": { status: 200, body: EMPTY_ACCESS },
    });
    renderModal();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));
    await screen.findByText(SECRET);

    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "reopen" }));

    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("");
  });

  it("will not create a token without a name", () => {
    stubFetchRoutes({});
    renderModal();
    expect(screen.getByRole("button", { name: "Create token" })).toBeDisabled();
  });

  // Pinned to the literal 80, not to MAX_TOKEN_NAME_LENGTH -- importing the
  // same constant the component uses would let this drift silently along
  // with it. 80 is api/internal/domain/api_token.go's MaxAPITokenNameLength,
  // the value ValidateAPITokenName actually enforces server-side.
  it("caps the name field at the domain's own MaxAPITokenNameLength, 80", () => {
    stubFetchRoutes({});
    renderModal();
    expect(screen.getByLabelText("Name")).toHaveAttribute("maxlength", "80");
  });
});
