// web/nginx.conf carries the browser security headers (HSTS, CSP, frame
// blocking, nosniff, Referrer-Policy) -- see the comment above them there for
// why each exists. Nothing else in the build would notice them disappearing:
// the app works exactly the same without them, so a "tidy-up" that deletes a
// line, or a new location block that quietly drops all of them, would ship
// green. This test is that notice.
//
// The inheritance rule it pins is the easy one to miss: nginx gives a location
// block the server's add_header lines ONLY if that location declares none of
// its own. One `add_header` inside `location /api/` would strip HSTS and CSP
// from every API response without any error or warning.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const WEB_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function withoutComments(text: string): string {
  return text
    .split("\n")
    .map((line) => line.replace(/#.*$/, ""))
    .join("\n");
}

const nginx = withoutComments(readFileSync(resolve(WEB_ROOT, "nginx.conf"), "utf8"));

// The bodies of every `location ... { ... }` block. No location in this file
// nests braces, so matching to the first closing brace is enough.
function locationBodies(conf: string): string[] {
  return [...conf.matchAll(/location\b[^{]*\{([^}]*)\}/g)].map((m) => m[1]);
}

function headerValue(conf: string, name: string): string | undefined {
  const match = conf.match(new RegExp(`add_header\\s+${name}\\s+"([^"]*)"\\s+always;`));
  return match?.[1];
}

describe("nginx security headers", () => {
  it.each([
    "Strict-Transport-Security",
    "Content-Security-Policy",
    "X-Frame-Options",
    "X-Content-Type-Options",
    "Referrer-Policy",
  ])("sets %s on every response, errors included", (name) => {
    expect(headerValue(nginx, name)).toBeTruthy();
  });

  it("declares no add_header inside a location, which would drop the server-level ones there", () => {
    for (const body of locationBodies(nginx)) {
      expect(body).not.toMatch(/\badd_header\b/);
    }
  });

  it("keeps the CSP strict: no inline script or eval, and no framing", () => {
    const csp = headerValue(nginx, "Content-Security-Policy") ?? "";
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("script-src 'self'");
    expect(csp).not.toContain("unsafe-inline");
    expect(csp).not.toContain("unsafe-eval");
  });

  it("hides the nginx version", () => {
    expect(nginx).toMatch(/server_tokens\s+off;/);
  });

  // The CSP forbids inline script, so index.html must not carry any: a
  // <script> without a src would be blocked in production and nowhere else.
  it("index.html has no inline script the CSP would block", () => {
    const html = readFileSync(resolve(WEB_ROOT, "index.html"), "utf8");
    const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)];
    for (const [, attributes, body] of scripts) {
      expect(attributes).toMatch(/\bsrc=/);
      expect(body.trim()).toBe("");
    }
  });
});
