import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";
import { I18nProvider } from "@/lib/i18n";
import { LANG_COOKIE, normaliseLang } from "@/lib/i18n/lang";
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

/**
 * The language is read here, on the server, from a cookie.
 *
 * `<html lang>` has to be right in the first byte of HTML: it is what screen
 * readers switch voice on and what the browser's own translation prompt
 * reads. Taking it from localStorage instead would mean rendering one
 * language, hydrating, and swapping — a flicker on every load.
 *
 * `metadata` above stays as it is. Translating it would mean replacing it
 * with an async `generateMetadata`, and a single canonical language is the
 * right answer for OpenGraph and search anyway.
 */
export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const lang = normaliseLang((await cookies()).get(LANG_COOKIE)?.value);

  return (
    <html lang={lang}>
      {/* No chrome here on purpose.
          There are three kinds of page now and they do not share a frame: the
          public landing has its own header, the sign-in screen has none, and
          the application has a sidebar. A header rendered here would appear
          on all three and belong to none. Each supplies its own. */}
      <body style={{ background: "var(--bg)", color: "var(--text)", minHeight: "100vh" }}>
        <I18nProvider initial={lang}>{children}</I18nProvider>
      </body>
    </html>
  );
}
