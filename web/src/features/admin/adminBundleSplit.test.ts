// Lazy loading (router.tsx's `lazy(() => import("../features/admin/..."))`)
// is a requirement here, not an optimisation -- the point is that no
// household member's browser ever downloads AdminShell or AdminFlagsPage's
// code, even though requirePlatformAdmin on the server is what actually
// keeps them out of the admin routes themselves.
//
// This walks only *static* `import`/`export ... from` edges from
// src/main.tsx, the app's real entry point, and never follows a dynamic
// `import()` call -- that is deliberate, not a limitation worked around:
// React.lazy's whole mechanism *is* a dynamic import, and the regression
// this test exists to catch is exactly someone replacing that with a plain
// top-level import in router.tsx. It does not invoke Rollup and cannot see
// what actually ends up as a separate chunk on disk; it proves the
// necessary precondition for that split -- nothing in main.tsx's static
// import graph resolves to either admin component file -- deterministically
// and without a build, not the chunking itself.
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SRC_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const ENTRY = join(SRC_ROOT, "main.tsx");

// Three shapes cover every static import/re-export form used in this
// codebase: `import ... from "x"`, a bare side-effect `import "x"`, and
// `export ... from "x"`. None of the three can match a dynamic `import(`
// call -- each requires either whitespace before the quoted specifier (a
// dynamic import's `(` follows `import` with no space) or the literal
// keyword `from`, which a dynamic import never carries.
const STATIC_IMPORT_PATTERNS = [
  /\bimport\s+[^'"()]*?from\s+['"]([^'"]+)['"]/g,
  /\bimport\s+['"]([^'"]+)['"]/g,
  /\bexport\s+[^'"()]*?from\s+['"]([^'"]+)['"]/g,
];

const RESOLUTION_SUFFIXES = ["", ".tsx", ".ts", "/index.tsx", "/index.ts"];

// Resolves a relative specifier to a real file under src/, or null for
// anything this walk doesn't need to follow: a bare package specifier
// (nothing under src/ to walk into) or a non-JS asset (index.css and the
// like -- this graph only cares about code).
function resolveSpecifier(fromFile: string, specifier: string): string | null {
  if (!specifier.startsWith(".")) return null;
  const base = resolve(dirname(fromFile), specifier);
  for (const suffix of RESOLUTION_SUFFIXES) {
    const candidate = `${base}${suffix}`;
    // isFile, not just existsSync: a bare "./admin" specifier with no
    // matching index file would otherwise resolve to the directory itself
    // (the "" suffix candidate) and crash the next readFileSync.
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  return null;
}

function staticDependenciesOf(file: string): string[] {
  const source = readFileSync(file, "utf8");
  const specifiers = new Set<string>();
  for (const pattern of STATIC_IMPORT_PATTERNS) {
    for (const match of source.matchAll(pattern)) {
      specifiers.add(match[1]);
    }
  }
  const resolved: string[] = [];
  for (const specifier of specifiers) {
    const target = resolveSpecifier(file, specifier);
    if (target) resolved.push(target);
  }
  return resolved;
}

function staticallyReachableFrom(entry: string): Set<string> {
  const seen = new Set<string>();
  const stack = [entry];
  while (stack.length > 0) {
    const file = stack.pop();
    if (file === undefined || seen.has(file)) continue;
    seen.add(file);
    for (const dependency of staticDependenciesOf(file)) {
      if (!seen.has(dependency)) stack.push(dependency);
    }
  }
  return seen;
}

describe("the main entry point's static import graph", () => {
  it("never statically reaches any admin page from main.tsx", () => {
    const reachable = [...staticallyReachableFrom(ENTRY)];

    // A regression here looks exactly like someone "simplifying" router.tsx
    // by swapping `lazy(() => import("../features/admin/AdminShell"))` for
    // a plain top-level `import { AdminShell } from
    // "../features/admin/AdminShell"` -- that one-line change is precisely
    // what would put either path below back into this list.
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminShell.tsx"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminFlagsPage.tsx"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminHouseholdsPage.tsx"),
    );
    // AdminHouseholdPage.tsx arrives in Task 9; the walk only checks
    // absence, so this assertion holds now and keeps holding.
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminHouseholdPage.tsx"),
    );
    // The same regression can arrive one layer down: router.tsx importing a
    // constant from useAdminDirectory.ts (or useAdmin.ts) rather than from a
    // leaf module drags the hook itself, and everything it imports, into
    // main.tsx's static graph even though no admin *component* is imported
    // directly. directoryLimits.ts exists so router.tsx never has to do
    // that (see its own header comment).
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "useAdminDirectory.ts"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "useAdmin.ts"),
    );
    // Task 7: AdminMailPage takes no props, so router.tsx never needs a
    // constant from useAdminOutbox.ts the way it needs
    // DIRECTORY_DEFAULT_LIMIT/DIRECTORY_MAX_LIMIT for the households route --
    // there is no leaf module for it to fall back on, so this assertion is
    // the one thing standing between a future route change and dragging the
    // whole outbox hook (and its schemas) into main.tsx's bundle.
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminMailPage.tsx"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "useAdminOutbox.ts"),
    );
    // The database browse. Unlike the outbox, its table route does need two
    // constants from router.tsx (validateSearch bounds the limit and offset
    // a hand-typed URL asks for) -- browseLimits.ts is the leaf module that
    // exists so router.tsx can have them without importing the hook file,
    // exactly as directoryLimits.ts does. useAdminDatabase.ts re-exports the
    // same two constants, so this assertion is what stands between a
    // convenient-looking import swap and the whole browse query layer
    // landing in every household member's bundle.
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminDatabasePage.tsx"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "useAdminDatabase.ts"),
    );
  });

  // A walk that is silently broken (a resolution bug, ENTRY pointing at the
  // wrong file) would "pass" the assertion above for the wrong reason: it
  // would find nothing reachable at all, admin files included. Anchoring on
  // a file every build genuinely needs makes that failure mode visible.
  it("does reach an ordinary route's component, proving the walk itself resolves real files", () => {
    const reachable = [...staticallyReachableFrom(ENTRY)];

    expect(reachable).toContain(
      join(SRC_ROOT, "features", "overview", "OverviewPage.tsx"),
    );
    expect(reachable).toContain(join(SRC_ROOT, "routes", "router.tsx"));
  });
});
