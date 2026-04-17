import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import pkg from "./package.json" with { type: "json" };

export default defineConfig({
  plugins: [react()],
  define: {
    __ONIBI_WEB_VERSION__: JSON.stringify(pkg.version)
  },
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.ts"]
  }
});
