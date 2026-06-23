import { defineConfig } from "vite";

export default defineConfig({
  build: {
    outDir: "../internal/web/static/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    manifest: true
  }
});
