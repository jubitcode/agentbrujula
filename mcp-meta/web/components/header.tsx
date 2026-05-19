"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { connectWallet, disconnect, getSavedWallet } from "@/lib/wallet";
import { prepareWallet } from "@/lib/marketplace";
import { BrujulaWordmark } from "@/components/logo/BrujulaWordmark";

// Stellar Expert explorer host — used to build the "view on-chain" link from
// the wallet dropdown. Aware of the configured network so dev (testnet) and
// production (public) both work without a code change.
const EXPLORER_BASE =
  (process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET").toUpperCase() === "PUBLIC"
    ? "https://stellar.expert/explorer/public"
    : "https://stellar.expert/explorer/testnet";

// The chrome is single-page: /hire is the entire product surface once you have
// a wallet. We do not render any nav items because nav with a single entry is
// noise. Other routes (/explore, /knowledge, /debate) remain URL-accessible
// for ops and demos but are hidden from the chrome.

export function Header() {
  const pathname = usePathname();
  const router = useRouter();
  const [walletKey, setWalletKey] = useState<string | null>(null);
  const [showDropdown, setShowDropdown] = useState(false);
  // null = not yet fetched, "" = lookup failed, else display string e.g. "0.45"
  const [balance, setBalance] = useState<string | null>(null);
  // Copy-feedback state: when the user copies the address, swap the icon
  // and label to a confirmation for 1.5s, then revert. Makes the click feel
  // acknowledged.
  const [copied, setCopied] = useState(false);

  const handleCopyAddress = async () => {
    if (!walletKey) return;
    try {
      await navigator.clipboard.writeText(walletKey);
    } catch {
      // Some browsers reject without user-gesture context; we still flash
      // success-style feedback so the panel doesn't feel dead — the user can
      // long-press the address manually if needed.
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  // Re-check wallet state on every route change
  useEffect(() => {
    setWalletKey(getSavedWallet());
  }, [pathname]);

  // Fetch the live USDC balance the first time the dropdown is opened. The
  // dropdown is the only place that shows it, so deferring the call avoids
  // hammering /api/prepare-wallet on every header render.
  useEffect(() => {
    if (!showDropdown || !walletKey || balance !== null) return;
    let cancelled = false;
    prepareWallet(walletKey)
      .then((r) => {
        if (!cancelled) setBalance(r.usdc_balance_display ?? "0");
      })
      .catch(() => {
        if (!cancelled) setBalance("");
      });
    return () => {
      cancelled = true;
    };
  }, [showDropdown, walletKey, balance]);

  const handleConnect = async () => {
    try {
      const { publicKey } = await connectWallet();
      setWalletKey(publicKey);
      setShowDropdown(false);
      // The header lives on every page, but the only place "Connect Wallet"
      // is offered is the landing (`/`). Once they have a wallet, push them
      // into the product — same behaviour as the hero CTA and as the auth
      // callback. Keeping them on the landing after success looks broken.
      router.push("/hire");
    } catch {
      // User cancelled — stay where we are, no redirect.
    }
  };

  const handleDisconnect = async () => {
    await disconnect();
    setWalletKey(null);
    setShowDropdown(false);
    // Send the user back to the landing. Without this, disconnecting on
    // /hire (the only authenticated page) leaves them on a page that
    // assumes a wallet they no longer have.
    router.push("/");
  };

  const truncated = walletKey
    ? `${walletKey.slice(0, 4)}...${walletKey.slice(-4)}`
    : null;

  return (
    <header style={{
      position: "sticky",
      top: 0,
      zIndex: 50,
      height: 52,
      background: "rgba(10,10,10,0.92)",
      backdropFilter: "blur(12px)",
      borderBottom: "1px solid var(--border)",
    }}>
      <div style={{
        maxWidth: 900,
        margin: "0 auto",
        padding: "0 1.5rem",
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
      }}>
        {/* Logo. When the user has a wallet saved we already know they have
            seen the landing — sending them back there on every brand click is
            friction, so the logo deep-links them straight into /hire. */}
        <Link
          href={walletKey ? "/hire" : "/"}
          style={{ display: "flex", alignItems: "center", textDecoration: "none", color: "var(--text)" }}
        >
          <BrujulaWordmark height={28} />
        </Link>

        {/* Wallet */}
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div style={{ position: "relative" }}>
          {walletKey ? (
            <button
              onClick={() => setShowDropdown(!showDropdown)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "5px 12px",
                fontSize: 11,
                fontFamily: "var(--mono)",
                color: "var(--muted)",
                background: "transparent",
                border: "1px solid var(--border2)",
                borderRadius: 20,
                cursor: "pointer",
                transition: "all 0.15s",
              }}
            >
              <span style={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                background: "var(--green)",
              }} />
              {truncated}
            </button>
          ) : (
            <button onClick={handleConnect} className="btn-primary" style={{ fontSize: 13, padding: "6px 16px" }}>
              Connect Wallet
            </button>
          )}

          {showDropdown && walletKey && (
            <div style={{
              position: "absolute",
              right: 0,
              top: "100%",
              marginTop: 10,
              width: 300,
              background: "var(--surface)",
              border: "1px solid var(--border)",
              borderRadius: "var(--r2)",
              padding: 0,
              zIndex: 50,
              boxShadow: "0 12px 32px rgba(0,0,0,0.4)",
              overflow: "hidden",
            }}>
              {/* HEADER — wallet icon + label + connection dot. Replaces the
                  raw "// wallet" mono string that looked unfinished. */}
              <div style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "14px 16px",
                borderBottom: "1px solid var(--border)",
              }}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none"
                     stroke="var(--accent)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
                     aria-hidden="true">
                  <path d="M21 12V7H5a2 2 0 0 1 0-4h14v4"/>
                  <path d="M3 5v14a2 2 0 0 0 2 2h16v-5"/>
                  <path d="M18 12a2 2 0 0 0 0 4h4v-4Z"/>
                </svg>
                <span style={{ fontSize: 12, fontWeight: 600, color: "var(--text)", letterSpacing: 0.2 }}>
                  Wallet
                </span>
                <span style={{ flex: 1 }} />
                <span title="Connected" style={{
                  width: 7, height: 7, borderRadius: "50%",
                  background: "var(--green)",
                  boxShadow: "0 0 0 2px rgba(34,197,94,0.18)",
                }} />
              </div>

              {/* BALANCE — the highest-priority info, big and centered.
                  Loading and error states are styled inline so the panel
                  size doesn't jump. */}
              <div style={{ padding: "16px 16px 6px" }}>
                {balance === null && (
                  <div style={{ fontSize: 13, color: "var(--dim)" }}>Loading balance…</div>
                )}
                {balance === "" && (
                  <div style={{ fontSize: 13, color: "var(--dim)" }}>— balance unavailable</div>
                )}
                {balance && balance !== "" && (
                  <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                    <span style={{
                      fontSize: 26, fontWeight: 600,
                      color: "var(--text)", fontFamily: "var(--mono)", lineHeight: 1,
                    }}>
                      {balance}
                    </span>
                    <span style={{ fontSize: 12, color: "var(--muted)" }}>USDC</span>
                  </div>
                )}
                <div style={{ fontSize: 11, color: "var(--dim)", marginTop: 4 }}>Available balance</div>
              </div>

              {/* ADDRESS — click to copy with full feedback loop.
                  Hover lifts the background; pressed scales down 2%;
                  post-click the icon swaps to a checkmark and the label
                  reads "Copied!" in green for 1.5s before reverting. */}
              <button
                type="button"
                onClick={handleCopyAddress}
                aria-label="Copy full wallet address"
                title="Click to copy full address"
                style={{
                  display: "flex", alignItems: "center", gap: 8,
                  width: "100%", textAlign: "left",
                  padding: "10px 16px",
                  background: copied ? "rgba(34,197,94,0.08)" : "transparent",
                  border: "none",
                  cursor: "pointer",
                  borderTop: "1px solid var(--border)",
                  borderBottom: "1px solid var(--border)",
                  transition: "background 180ms ease, transform 120ms ease",
                }}
                onMouseEnter={(e) => {
                  if (!copied) e.currentTarget.style.background = "var(--surface2)";
                }}
                onMouseLeave={(e) => {
                  if (!copied) e.currentTarget.style.background = "transparent";
                }}
                onMouseDown={(e) => { e.currentTarget.style.transform = "scale(0.98)"; }}
                onMouseUp={(e)   => { e.currentTarget.style.transform = "scale(1)"; }}
              >
                {copied ? (
                  // Checkmark — drawn fresh so it animates in.
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none"
                       stroke="var(--green)" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"
                       aria-hidden="true">
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                ) : (
                  // Two-overlapping-squares "copy" glyph.
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none"
                       stroke="var(--muted)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
                       aria-hidden="true">
                    <rect x="9" y="9" width="13" height="13" rx="2"/>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
                  </svg>
                )}
                <span style={{
                  fontSize: 11, fontFamily: "var(--mono)",
                  color: copied ? "var(--green)" : "var(--muted)",
                  transition: "color 180ms ease",
                }}>
                  {`${walletKey.slice(0, 6)}…${walletKey.slice(-6)}`}
                </span>
                <span style={{ flex: 1 }} />
                <span style={{
                  fontSize: 10,
                  color: copied ? "var(--green)" : "var(--dim)",
                  fontWeight: copied ? 600 : 400,
                  transition: "color 180ms ease",
                }}>
                  {copied ? "Copied!" : "Copy"}
                </span>
              </button>

              {/* ACTIONS — explorer link + disconnect, each with an icon. */}
              <a
                href={`${EXPLORER_BASE}/account/${walletKey}`}
                target="_blank"
                rel="noreferrer noopener"
                style={{
                  display: "flex", alignItems: "center", gap: 10,
                  padding: "12px 16px",
                  fontSize: 13, color: "var(--text)",
                  textDecoration: "none",
                  transition: "background 0.15s",
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = "var(--surface2)")}
                onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none"
                     stroke="var(--accent)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
                     aria-hidden="true">
                  <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
                  <polyline points="15 3 21 3 21 9"/>
                  <line x1="10" y1="14" x2="21" y2="3"/>
                </svg>
                View on Stellar Expert
              </a>

              <button
                onClick={handleDisconnect}
                style={{
                  display: "flex", alignItems: "center", gap: 10,
                  width: "100%", textAlign: "left",
                  padding: "12px 16px",
                  background: "transparent", border: "none",
                  borderTop: "1px solid var(--border)",
                  color: "#ef4444",
                  fontSize: 13,
                  cursor: "pointer",
                  transition: "background 0.15s",
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = "rgba(239,68,68,0.08)")}
                onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none"
                     stroke="#ef4444" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
                     aria-hidden="true">
                  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
                  <polyline points="16 17 21 12 16 7"/>
                  <line x1="21" y1="12" x2="9" y2="12"/>
                </svg>
                Disconnect
              </button>
            </div>
          )}
        </div>
        </div>
      </div>
    </header>
  );
}
