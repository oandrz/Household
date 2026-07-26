// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the role-driven capability forcing (an owner invite must
// carry every capability -- domain.ErrOwnerMustHoldAllCapabilities) and the
// "marriage not offered to a Kid" rule are non-trivial and untested
// otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { InviteMemberModal } from "./InviteMemberModal";

const INVITE_URL = "/api/v1/household/members/invite";

function renderModal() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <InviteMemberModal open onClose={() => {}} />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("InviteMemberModal", () => {
  it("does not offer the Marriage space capability for the default Kid role", () => {
    renderModal();
    expect(screen.queryByText("Marriage space")).not.toBeInTheDocument();
  });

  it("shows Marriage space, forced on and disabled, once Parent is selected", () => {
    renderModal();
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "owner" } });

    expect(screen.getByText("Marriage space")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Marriage space access" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Marriage space access" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    // domain.ErrOwnerMustHoldAllCapabilities -- an owner invite must force
    // every other capability on too, not just marriage.
    expect(screen.getByRole("switch", { name: "Calendar access" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Money & balances access" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("shows the design's 'off for kids by default' helper text on the money row", () => {
    renderModal();
    expect(screen.getByText("Off for kids by default")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Money & balances access" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("submits POST /api/v1/household/members/invite with the kid's default capabilities", async () => {
    const fetchMock = stubFetchRoutes({
      [`POST ${INVITE_URL}`]: { status: 201, body: { status: "invited" } },
    });
    renderModal();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Kayla" } });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === INVITE_URL && (init?.method ?? "").toUpperCase() === "POST",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({
        name: "Kayla",
        email: "",
        role: "limited",
        capabilities: ["calendar", "chores"],
      });
    });
  });

  it("submits every capability when the role is Parent", async () => {
    const fetchMock = stubFetchRoutes({
      [`POST ${INVITE_URL}`]: { status: 201, body: { status: "invited" } },
    });
    renderModal();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Christine" } });
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "owner" } });
    fireEvent.change(screen.getByLabelText(/Email or phone/), {
      target: { value: "christine@hearth.family" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === INVITE_URL && (init?.method ?? "").toUpperCase() === "POST",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({
        name: "Christine",
        email: "christine@hearth.family",
        role: "owner",
        capabilities: ["calendar", "chores", "money", "marriage"],
      });
    });
  });
});
