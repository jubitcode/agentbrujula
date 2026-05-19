import type { MetadataRoute } from "next";

// Generated as /sitemap.xml at the site root. Two public surfaces today:
// the landing (`/`) and the product (`/hire`). Hidden helpers do not belong
// in the sitemap so search engines don't waste crawl budget on them.

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();
  return [
    {
      url: "https://agentbrujula.com/",
      lastModified: now,
      changeFrequency: "weekly",
      priority: 1.0,
    },
    {
      url: "https://agentbrujula.com/hire",
      lastModified: now,
      changeFrequency: "weekly",
      priority: 0.9,
    },
  ];
}
