import type { MetadataRoute } from "next";

// Generated as /robots.txt at the site root. Tells crawlers what to index.
// We want everything indexable — Brújula is a public product. Authenticated
// helper routes (/explore, /knowledge, /debate, /deploy) are kept out of the
// crawl because they have no SEO value and would dilute the landing's PageRank.

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        allow: "/",
        disallow: ["/explore", "/knowledge", "/debate", "/deploy", "/auth/", "/task/"],
      },
    ],
    sitemap: "https://agentbrujula.com/sitemap.xml",
    host: "https://agentbrujula.com",
  };
}
