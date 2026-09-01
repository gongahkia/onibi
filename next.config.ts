import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Allows iPhones on this private LAN to load development HMR assets. Production
  // requests are unaffected; do not widen this to a public wildcard.
  allowedDevOrigins: ["192.168.88.9"],
  async headers() {
    return [
      {
        source: "/sw.js",
        headers: [
          { key: "Cache-Control", value: "no-cache, no-store, must-revalidate" },
          { key: "Content-Type", value: "application/javascript; charset=utf-8" }
        ]
      }
    ];
  }
};

export default nextConfig;
