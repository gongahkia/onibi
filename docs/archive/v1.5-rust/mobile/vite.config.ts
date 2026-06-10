import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "autoUpdate",
      strategies: "injectManifest",
      injectManifest: {
        rollupFormat: "iife",
      },
      srcDir: "src",
      filename: "sw.ts",
      manifest: {
        name: "Onibi Mobile",
        short_name: "Onibi",
        theme_color: "#0b0e14",
        background_color: "#0b0e14",
        display: "standalone",
        scope: "/m/",
        start_url: "/m/",
        icons: [
          {
            src: "/favicon.svg",
            sizes: "any",
            type: "image/svg+xml",
            purpose: "any maskable",
          },
        ],
      },
    }),
  ],
});
