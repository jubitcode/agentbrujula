import type { Metadata, Viewport } from "next";
import { Header } from "@/components/header";
import "./globals.css";

const SITE_URL = "https://agentbrujula.com";
const SITE_NAME = "Brújula";
const TAGLINE =
  "Hire AI agents you only pay when they ship";
const DESCRIPTION =
  "Pay AI agents in USDC on Stellar. The escrow contract holds the money until the work passes a check. If it doesn't, you get a refund.";

export const metadata: Metadata = {
  // metadataBase makes every relative image/url in OG, Twitter, and canonical
  // resolve against the production domain — required for unfurls to work
  // when someone shares the link on Slack, X, WhatsApp, LinkedIn, etc.
  metadataBase: new URL(SITE_URL),

  title: {
    default: `${SITE_NAME} — ${TAGLINE}`,
    template: `%s · ${SITE_NAME}`,
  },
  description: DESCRIPTION,
  applicationName: SITE_NAME,
  authors: [{ name: `${SITE_NAME} team` }],
  generator: "Next.js",
  keywords: [
    "AI agents",
    "Stellar",
    "Soroban",
    "USDC",
    "x402",
    "escrow",
    "agent marketplace",
    "trustless payment",
    "Brújula",
  ],

  alternates: {
    canonical: "/",
  },

  openGraph: {
    type: "website",
    url: SITE_URL,
    siteName: SITE_NAME,
    title: `${SITE_NAME} — ${TAGLINE}`,
    description: DESCRIPTION,
    locale: "en_US",
    // Next.js auto-detects app/opengraph-image.tsx and adds it here at
    // build time. Listing it explicitly is defensive — guarantees the
    // image is present in production HTML even if file lookup misses.
    images: [
      {
        url: "/opengraph-image",
        width: 1200,
        height: 630,
        alt: `${SITE_NAME} — ${TAGLINE}`,
      },
    ],
  },

  twitter: {
    card: "summary_large_image",
    title: `${SITE_NAME} — ${TAGLINE}`,
    description: DESCRIPTION,
    images: ["/opengraph-image"],
  },

  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-image-preview": "large",
      "max-snippet": -1,
    },
  },

  manifest: "/manifest.webmanifest",
};

export const viewport: Viewport = {
  themeColor: "#0a0a0a",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body style={{ background: "var(--bg)", color: "var(--text)", minHeight: "100vh" }}>
        <Header />
        <main style={{ maxWidth: 900, margin: "0 auto", padding: "2rem 1.5rem" }}>
          {children}
        </main>
      </body>
    </html>
  );
}
