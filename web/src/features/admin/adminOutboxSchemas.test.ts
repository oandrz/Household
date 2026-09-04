import { describe, expect, it } from "vitest";
import {
  adminMailListSchema,
  adminMailMessageSchema,
} from "./adminOutboxSchemas";

describe("the outbox schemas", () => {
  it("parses the list shape the backend sends", () => {
    const parsed = adminMailListSchema.parse({
      messages: [
        {
          id: "0OQ1sV2mB7hN4kR8xT3wZq",
          to: "chris@example.com",
          subject: "Your Hearth sign-in link",
          sentAt: "2026-09-04T09:12:33Z",
        },
      ],
      total: 9,
      truncated: true,
    });
    expect(parsed.messages[0].to).toBe("chris@example.com");
  });

  // .strict() everywhere, for the same reason the directory schemas are:
  // a key the backend did not promise must fail the parse rather than reach
  // a screen. Here the key that must never appear is a body on a list row.
  it("refuses a listed message that carries a body", () => {
    expect(() =>
      adminMailListSchema.parse({
        messages: [
          {
            id: "0OQ1sV2mB7hN4kR8xT3wZq",
            to: "chris@example.com",
            subject: "Your Hearth sign-in link",
            sentAt: "2026-09-04T09:12:33Z",
            text: "Open https://oink.mywire.org/sign-in/magic?token=abc123",
          },
        ],
        total: 1,
        truncated: false,
      }),
    ).toThrow();
  });

  it("refuses a message that carries an html part", () => {
    expect(() =>
      adminMailMessageSchema.parse({
        id: "0OQ1sV2mB7hN4kR8xT3wZq",
        to: "chris@example.com",
        subject: "Your Hearth sign-in link",
        sentAt: "2026-09-04T09:12:33Z",
        links: [],
        text: "",
        html: "<p>never</p>",
      }),
    ).toThrow();
  });
});
