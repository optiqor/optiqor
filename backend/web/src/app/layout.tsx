import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { SiteHeader } from "@/components/site-header";
import { SiteFooter } from "@/components/site-footer";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
  display: "swap",
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL("https://optiqor.dev"),
  title: {
    default: "Optiqor — Detect. Fix. Prove.",
    template: "%s · Optiqor",
  },
  description:
    "Kubernetes Helm cost optimization from your terminal and your PRs. Deterministic detectors, signed receipts of realized savings.",
  openGraph: {
    title: "Optiqor — Detect. Fix. Prove.",
    description:
      "Cost optimization for Kubernetes Helm charts. Deterministic. Cryptographically verifiable.",
    url: "https://optiqor.dev",
    siteName: "Optiqor",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "Optiqor",
    description: "Detect. Fix. Prove.",
  },
  icons: {
    icon: "/favicon.ico",
  },
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col">
        <SiteHeader />
        <main className="flex-1 load-in">{children}</main>
        <SiteFooter />
      </body>
    </html>
  );
}
