import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
    include: ["src/__tests__/**/*.test.ts"],
    coverage: {
      reporter: ["text"],
      include: ["src/**/*.ts"],
      exclude: ["src/**/*.test.ts", "src/e2e.test.ts", "src/wake-lock.test.ts", "src/sw.ts"]
    }
  }
});
