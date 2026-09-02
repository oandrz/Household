import { describe, expect, it } from "vitest";
import {
  adminHouseholdPageSchema,
  adminHouseholdsResponseSchema,
} from "./adminDirectorySchemas";

const listing = {
  id: "h1",
  name: "Oentoro",
  familyName: "Oentoro",
  memberCount: 4,
  createdAt: "2026-08-15T02:11:09Z",
  lastActiveAt: null,
  primaryCurrency: "SGD",
  match: null,
};
const response = {
  metrics: {
    households: 1,
    activeHouseholds7d: 0,
    signups30d: { requested: 0, completed: 0 },
    pendingInvites: 0,
  },
  households: [listing],
  truncated: false,
};

describe("adminDirectorySchemas", () => {
  it("accepts the spec's list shape", () => {
    expect(
      adminHouseholdsResponseSchema.parse(response).households[0].name,
    ).toBe("Oentoro");
  });
  // .strict(): a money field added to a DTO by accident must fail here, not
  // pass through to a screen the spec says never shows money.
  it("rejects an unexpected key on a household row", () => {
    const withBalance = {
      ...response,
      households: [{ ...listing, balance: 100 }],
    };
    expect(() => adminHouseholdsResponseSchema.parse(withBalance)).toThrow();
  });
  it("rejects a missing nullable field rather than defaulting it", () => {
    // Rest-destructure to drop lastActiveAt; the binding itself is never
    // read -- only the omission from withoutLastActive matters below.
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { lastActiveAt: _dropped, ...withoutLastActive } = listing;
    expect(() =>
      adminHouseholdsResponseSchema.parse({
        ...response,
        households: [withoutLastActive],
      }),
    ).toThrow();
  });
  it("accepts a Telegram-only member and a null lockout on the drill-in", () => {
    const page = {
      household: {
        id: "h1",
        name: "O",
        familyName: "O",
        createdAt: "2026-08-15T02:11:09Z",
        primaryCurrency: "SGD",
      },
      members: [
        {
          userId: "u1",
          name: "Kid",
          email: null,
          channel: "telegram",
          role: "limited",
          capabilities: ["calendar"],
          lastActiveAt: null,
        },
      ],
      pendingInvites: [],
      lockout: null,
    };
    expect(adminHouseholdPageSchema.parse(page).members[0].channel).toBe(
      "telegram",
    );
  });
  it("rejects a channel the API never produces", () => {
    const page = {
      household: {
        id: "h1",
        name: "O",
        familyName: "O",
        createdAt: "2026-08-15T02:11:09Z",
        primaryCurrency: "SGD",
      },
      members: [
        {
          userId: "u1",
          name: "Kid",
          email: null,
          channel: "carrier-pigeon",
          role: "limited",
          capabilities: [],
          lastActiveAt: null,
        },
      ],
      pendingInvites: [],
      lockout: null,
    };
    expect(() => adminHouseholdPageSchema.parse(page)).toThrow();
  });
});
