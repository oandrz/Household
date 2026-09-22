import { describe, expect, it } from "vitest";
import type { MemberView, PendingInvite } from "../settings/schemas";
import { partnerStep } from "./partnerStep";

function member(role: string, id: string): MemberView {
  return {
    id,
    user: { id: `u-${id}`, email: "", displayName: id, avatarInitial: id[0].toUpperCase() },
    role,
    capabilities: [],
  };
}

function invite(role: string): PendingInvite {
  return {
    id: `inv-${role}`,
    name: "Jane",
    email: "jane@example.com",
    role,
    capabilities: [],
    channel: "email",
    expiresAt: "2026-09-26T09:00:00Z",
  };
}

describe("partnerStep", () => {
  it("is joined once a second owner exists", () => {
    expect(partnerStep([member("owner", "a"), member("owner", "c")], [])).toBe("joined");
  });

  it("is invited while an owner-role invite is pending", () => {
    expect(partnerStep([member("owner", "a")], [invite("owner")])).toBe("invited");
  });

  // Kids do not unlock Agreements, so neither a limited member nor a pending
  // limited invite moves this step.
  it("ignores limited members and limited invites", () => {
    expect(partnerStep([member("owner", "a"), member("limited", "k")], [invite("limited")])).toBe("none");
  });
});
