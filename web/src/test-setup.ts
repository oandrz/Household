// Registers @testing-library/jest-dom's matchers (toBeInTheDocument, etc.)
// against Vitest's expect. Every test file gets this for free via
// vitest.config.ts's setupFiles.
import "@testing-library/jest-dom/vitest";
