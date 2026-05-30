import react from "@vitejs/plugin-react";
import { configDefaults, defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "react-vendor",
              test: /node_modules[\\/](react|react-dom)[\\/]/u,
              priority: 40
            },
            {
              name: "flow-vendor",
              test: /node_modules[\\/](@xyflow|d3-|d3)[\\/]/u,
              priority: 30
            },
            {
              name: "icons-vendor",
              test: /node_modules[\\/]lucide-react[\\/]/u,
              priority: 20
            },
            {
              name: "workflow-spec",
              test: /packages[\\/]workflow-spec[\\/]/u,
              priority: 10
            },
            {
              name: "vendor",
              test: /node_modules/u,
              priority: 0
            }
          ]
        }
      }
    }
  },
  server: {
    proxy: {
      "/api": process.env.KELPCLAW_API_TARGET ?? "http://127.0.0.1:8787"
    }
  },
  test: {
    environment: "jsdom",
    exclude: [...configDefaults.exclude, "e2e/**"],
    setupFiles: "./test/setup.ts"
  }
});
