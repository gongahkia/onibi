import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Together Budget",
  description: "Shared spending, budgets, and goals for two.",
  manifest: "/manifest.webmanifest",
  appleWebApp: { capable: true, statusBarStyle: "default", title: "Together" },
  icons: { apple: "/icon.svg", icon: "/icon.svg" }
};

export const viewport: Viewport = { themeColor: "#6546e8", width: "device-width", initialScale: 1 };

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en"><body>{children}</body></html>;
}
