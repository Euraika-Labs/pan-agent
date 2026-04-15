import { defineConfig } from "vitest/config";

// Vitest picks up every *.test.ts in the tree by default. We explicitly
// exclude Playwright's tests/e2e/ directory because those use a separate
// runner with `test.describe` semantics that collide with vitest's own
// export — sharing the file globs would have vitest try to execute
// Playwright specs and fail at collection.
export default defineConfig({
  test: {
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    exclude: ["node_modules", "dist", "tests/e2e"],
  },
});
