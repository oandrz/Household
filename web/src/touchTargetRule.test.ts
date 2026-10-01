// A control that is 44px tall on a phone stays 44px tall on a tablet, and only
// goes compact from the `lg` breakpoint (1024px), where a pointer rather than
// a thumb is the likely input. components/fieldClasses.ts states the rule.
//
// Nothing else would notice it being broken: `min-h-11 sm:min-h-0` looks
// right, passes every other test, and is correct on a phone and on a desktop.
// It is only wrong between 640 and 1023px, on a touch tablet, which is the
// width nobody opens while developing. This rule was once written that way at
// 189 places. This test is the notice.
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SRC = dirname(fileURLToPath(import.meta.url));

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    const isSource = /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name);
    return isSource ? [path] : [];
  });
}

// The 44px floor, however a control spells it.
const FLOOR = /(?<![\w:-])(min-h-11|h-11|w-11|before:h-11)(?![\w-])/;
// The classes that take the floor away again. At `sm` they do it too early.
const RESET_AT_SM = /(?<![\w-])sm:(min-h-0|py-1\.5|h-7|w-7|h-auto|before:hidden)(?![\w.-])/;

function isComment(line: string): boolean {
  const text = line.trim();
  return text.startsWith("//") || text.startsWith("*") || text.startsWith("/*") || text.startsWith("{/*");
}

describe("the 44px touch floor", () => {
  it("is never taken away at the sm breakpoint, only from lg", () => {
    const offenders = sourceFiles(SRC).flatMap((file) =>
      readFileSync(file, "utf8")
        .split("\n")
        .map((line, index) => ({ line, where: `${relative(SRC, file)}:${index + 1}` }))
        .filter(({ line }) => !isComment(line) && FLOOR.test(line) && RESET_AT_SM.test(line))
        .map(({ where }) => where),
    );

    expect(offenders).toEqual([]);
  });

  // Guards the guard: if the sweep above ever stops finding the pattern at
  // all, it would pass on a tree full of offenders.
  it("still recognises the pattern it exists to refuse", () => {
    const old = 'className="min-h-11 rounded-lg px-3 py-2.5 sm:min-h-0 sm:py-1.5"';
    const current = 'className="min-h-11 rounded-lg px-3 py-2.5 lg:min-h-0 sm:w-auto lg:py-1.5"';

    expect(FLOOR.test(old) && RESET_AT_SM.test(old)).toBe(true);
    expect(FLOOR.test(current) && RESET_AT_SM.test(current)).toBe(false);
  });
});
