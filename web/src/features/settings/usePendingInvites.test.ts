import { describe, expect, it } from "vitest";
import { invitePollInterval } from "./usePendingInvites";
import { pendingInviteSchema, type PendingInvite } from "./schemas";

describe("pendingInviteSchema", () => {
  // Milestone 1's server sends neither field. The frontend must read that
  // as an email invite with no knock, so the two milestones deploy
  // independently.
  it("reads a row from a server that predates channel and knock", () => {
    const row = pendingInviteSchema.parse({
      id: "1", name: "Jane", email: "jane@example.com", role: "owner",
      capabilities: ["money"], expiresAt: "2026-09-27T00:00:00Z",
    });
    expect(row.channel).toBe("email");
    expect(row.knock ?? null).toBeNull();
  });

  it("reads a knock whose Telegram username is null", () => {
    const row = pendingInviteSchema.parse({
      id: "1", name: "Christine", email: "", role: "owner",
      capabilities: ["money"], expiresAt: "2026-09-21T00:00:00Z",
      channel: "telegram",
      knock: { username: null, code: "4812", knockedAt: "2026-09-20T10:00:00Z" },
    });
    expect(row.knock?.username ?? null).toBeNull();
    expect(row.knock?.code).toBe("4812");
  });
});

describe("invitePollInterval", () => {
  // Not `as const`: that would freeze `capabilities` to a readonly tuple,
  // which PendingInvite's plain `string[]` then refuses to accept.
  const waiting: PendingInvite = { id: "1", name: "C", email: "", role: "owner", capabilities: [],
    expiresAt: "", channel: "telegram", knock: null };
  const knocked: PendingInvite = { ...waiting, id: "2", knock: { username: "c_t", code: "4812", knockedAt: "" } };
  const emailRow: PendingInvite = { ...waiting, id: "3", channel: "email" };

  // The poll exists to catch a knock. Once there is one, or once there is
  // nothing that could produce one, it stops -- the TelegramPanel rule,
  // asserted against the exported function rather than over real elapsed
  // time (TelegramPanel.test.tsx says why).
  it("polls only while a Telegram invite is still waiting for its knock", () => {
    expect(invitePollInterval([waiting])).toBe(3000);
    expect(invitePollInterval([waiting, knocked])).toBe(3000);
    expect(invitePollInterval([knocked])).toBe(false);
    expect(invitePollInterval([emailRow])).toBe(false);
    expect(invitePollInterval([])).toBe(false);
    expect(invitePollInterval(undefined)).toBe(false);
  });
});
