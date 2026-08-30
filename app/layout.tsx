import type { Metadata, Viewport } from "next";
import { Inter, Lora, Nunito } from "next/font/google";
import "./globals.css";
import "./actions.css";
import "./motion.css";
import PwaRegister from "./pwa-register";

export const metadata: Metadata = {
  title: "Together Budget",
  description: "Shared spending, budgets, and goals for two.",
  manifest: "/manifest.webmanifest",
  appleWebApp: { capable: true, statusBarStyle: "default", title: "Together" },
  icons: { apple: "/icon.svg", icon: "/icon.svg" }
};

export const viewport: Viewport = { themeColor: "#6546e8", width: "device-width", initialScale: 1, viewportFit: "cover" };

const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const nunito = Nunito({ subsets: ["latin"], variable: "--font-nunito", display: "swap" });
const lora = Lora({ subsets: ["latin"], variable: "--font-lora", display: "swap" });

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en" className={`${inter.variable} ${nunito.variable} ${lora.variable}`}><body>{children}<PwaRegister /></body></html>;
}
