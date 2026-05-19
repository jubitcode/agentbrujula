"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const LINKS = [
  { href: "/hire", label: "Research" },
  { href: "/debate", label: "Debate" },
  { href: "/explore", label: "Explore" },
  { href: "/deploy", label: "Deploy" },
];

export function WorkspaceNav() {
  const pathname = usePathname();

  return (
    <nav style={{
      display: "flex",
      alignItems: "center",
      gap: 3,
      marginBottom: 32,
      paddingBottom: 16,
      borderBottom: "1px solid var(--border)",
      background: "var(--surface)",
      border: "1px solid var(--border)",
      borderRadius: "var(--r)",
      padding: 3,
    }}>
      {LINKS.map(({ href, label }) => (
        <Link
          key={href}
          href={href}
          style={{
            padding: "8px 16px",
            fontSize: 13,
            borderRadius: "var(--r)",
            textDecoration: "none",
            transition: "all 0.15s",
            color: pathname === href ? "var(--text)" : "var(--muted)",
            background: pathname === href ? "var(--surface2)" : "transparent",
            fontWeight: pathname === href ? 500 : 400,
          }}
        >
          {label}
        </Link>
      ))}
    </nav>
  );
}
