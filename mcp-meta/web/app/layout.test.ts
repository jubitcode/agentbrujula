/**
 * Metadata is what shows up in the browser tab, when someone shares the link
 * on Slack / X / WhatsApp, and when search engines index the page. The
 * production bug we're guarding against here is the chrome saying
 * "mcp-meta" — that name is irrelevant to the product and confuses users.
 *
 * These tests pin the public identity to "Brújula" + the agentbrujula.com
 * canonical URL so future refactors can't quietly regress it.
 */

import { metadata } from "./layout";

describe("Root metadata", () => {
  test("page title is Brújula (with accent)", () => {
    const title = metadata.title;
    if (typeof title === "string") {
      expect(title).toMatch(/Brújula/);
    } else if (title && typeof title === "object" && "default" in title) {
      expect(String(title.default)).toMatch(/Brújula/);
    } else {
      throw new Error(`unexpected title shape: ${JSON.stringify(title)}`);
    }
  });

  test("sub-pages use the '%s · Brújula' template (so /hire reads as 'Hire · Brújula')", () => {
    const title = metadata.title;
    if (title && typeof title === "object" && "template" in title) {
      expect(title.template).toMatch(/Brújula/);
      expect(title.template).toMatch(/%s/);
    } else {
      throw new Error("metadata.title must be an object with a template");
    }
  });

  test("description mentions the value prop in user-facing language", () => {
    expect(metadata.description).toBeTruthy();
    expect(String(metadata.description).toLowerCase()).toMatch(/usdc|stellar|agent/);
  });

  test("metadataBase points to agentbrujula.com (canonical domain)", () => {
    expect(metadata.metadataBase).toBeTruthy();
    expect(String(metadata.metadataBase)).toMatch(/agentbrujula\.com/);
  });

  test("OpenGraph block exists for link unfurls (Slack / X / WhatsApp / LinkedIn)", () => {
    expect(metadata.openGraph).toBeTruthy();
    expect(metadata.openGraph?.siteName).toMatch(/Brújula/);
    expect(metadata.openGraph?.images).toBeTruthy();
  });

  test("Twitter card metadata exists (so X embeds render the OG image, not a plain link)", () => {
    expect(metadata.twitter).toBeTruthy();
    expect(metadata.twitter?.card).toBe("summary_large_image");
  });

  test("robots allows indexing — we WANT search engines to find us", () => {
    expect(metadata.robots).toBeTruthy();
    const robots = metadata.robots as { index?: boolean; follow?: boolean };
    expect(robots.index).toBe(true);
    expect(robots.follow).toBe(true);
  });

  test("no leftover 'mcp-meta' branding anywhere in metadata", () => {
    const blob = JSON.stringify(metadata);
    expect(blob).not.toMatch(/mcp-meta/i);
  });
});
