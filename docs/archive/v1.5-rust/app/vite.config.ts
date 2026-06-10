import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// @ts-expect-error process is a nodejs global
const host = process.env.TAURI_DEV_HOST;

// https://vite.dev/config/
export default defineConfig(async () => ({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: "./src/vitest.setup.ts",
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return;
          }

          if (
            id.includes("@codemirror/lang-javascript") ||
            id.includes("@lezer/javascript")
          ) {
            return "vendor-editor-javascript";
          }

          if (id.includes("@codemirror/lang-html") || id.includes("@lezer/html")) {
            return "vendor-editor-html";
          }

          if (
            id.includes("@codemirror/lang-markdown") ||
            id.includes("@lezer/markdown")
          ) {
            return "vendor-editor-markdown";
          }

          if (id.includes("@codemirror/lang-css") || id.includes("@lezer/css")) {
            return "vendor-editor-css";
          }

          if (id.includes("@codemirror/lang-python")) {
            return "vendor-editor-python";
          }

          if (id.includes("@codemirror/lang-rust")) {
            return "vendor-editor-rust";
          }

          if (id.includes("@codemirror/lang-yaml")) {
            return "vendor-editor-yaml";
          }

          if (id.includes("@codemirror/lang-json")) {
            return "vendor-editor-json";
          }

          if (id.includes("@codemirror/view")) {
            return "vendor-editor-view";
          }

          if (id.includes("@codemirror/state")) {
            return "vendor-editor-state";
          }

          if (
            id.includes("@codemirror/commands") ||
            id.includes("@codemirror/search") ||
            id.includes("@codemirror/autocomplete") ||
            id.includes("@codemirror/lint")
          ) {
            return "vendor-editor-tools";
          }

          if (
            id.includes("@codemirror/language") ||
            id.includes("@lezer/common") ||
            id.includes("@lezer/highlight") ||
            id.includes("@lezer/lr")
          ) {
            return "vendor-editor-language";
          }

          if (id.includes("/codemirror")) {
            return "vendor-editor-setup";
          }

          if (
            id.includes("@codemirror") ||
            id.includes("@lezer")
          ) {
            return "vendor-editor-core";
          }

          if (id.includes("@xterm")) {
            return "vendor-terminal";
          }

          if (id.includes("react") || id.includes("zustand")) {
            return "vendor-ui";
          }

          if (id.includes("@tauri-apps")) {
            return "vendor-tauri";
          }

          return "vendor";
        },
      },
    },
  },

  // Vite options tailored for Tauri development and only applied in `tauri dev` or `tauri build`
  //
  // 1. prevent Vite from obscuring rust errors
  clearScreen: false,
  // 2. tauri expects a fixed port, fail if that port is not available
  server: {
    port: 1420,
    strictPort: true,
    host: host || false,
    hmr: host
      ? {
          protocol: "ws",
          host,
          port: 1421,
        }
      : undefined,
    watch: {
      // 3. tell Vite to ignore watching `src-tauri`
      ignored: ["**/src-tauri/**"],
    },
  },
}));
