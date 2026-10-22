import type { NextConfig } from "next";

const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:9876";
const marketplaceUrl = process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050";
const horizonUrl = process.env.NEXT_PUBLIC_HORIZON_URL || "https://horizon-testnet.stellar.org";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiUrl}/:path*`,
      },
    ];
  },
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: [
          {
            key: "Content-Security-Policy",
            value: [
              "default-src 'self'",
              `connect-src 'self' ${apiUrl} ${marketplaceUrl} ${horizonUrl}`.trim(),
              "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
              // Google Fonts: the Cinzel wordmark is @import-ed from inside
              // components/logo/*. Without these two the brand font silently
              // falls back to a serif default on every page.
              "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
              "font-src 'self' https://fonts.gstatic.com",
              "img-src 'self' data: https://stellar.creit.tech https://uni.onekey-asset.com",
              // Remotion's Player mounts a silent data: audio element to drive
              // frame timing. Without media-src it falls back to default-src
              // and the player renders a blank frame.
              "media-src 'self' data: blob:",
              "frame-ancestors 'none'",
            ].join("; "),
          },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
        ],
      },
    ];
  },
};

export default nextConfig;
