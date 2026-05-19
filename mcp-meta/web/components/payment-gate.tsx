"use client";

import { useState } from "react";
import { connectWallet, signTransaction, getSavedWallet } from "@/lib/wallet";
import { buildGenericPayment, prepareWallet, type PaymentInfo } from "@/lib/marketplace";
import { HORIZON_URL } from "@/lib/stellar-env";

interface PaymentGateProps {
  paymentInfo: PaymentInfo;
  onPaymentComplete: (paymentSignature: string) => void;
  onCancel: () => void;
  deadlineSeconds?: number;
  label?: string;
}

export function PaymentGate({
  paymentInfo,
  onPaymentComplete,
  onCancel,
  deadlineSeconds = 300,
  label = "Pay and execute",
}: PaymentGateProps) {
  const [loading, setLoading] = useState(false);
  const [step, setStep] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [walletKey, setWalletKey] = useState<string | null>(getSavedWallet());

  const cost = paymentInfo.estimated_cost || paymentInfo.price || "0.10";
  const ceiling = paymentInfo.budget_ceiling;

  const handlePay = async () => {
    setLoading(true);
    setError(null);

    try {
      let key = walletKey;
      if (!key) {
        setStep("Connecting wallet...");
        const w = await connectWallet();
        key = w.publicKey;
        setWalletKey(key);
      }

      setStep("Checking wallet...");
      const status = await prepareWallet(key);
      if (!status.has_trustline && status.trustline_xdr) {
        setStep("Signing trustline...");
        const signedTrustline = await signTransaction(status.trustline_xdr);
        const formData = new URLSearchParams({ tx: signedTrustline });
        const res = await fetch(`${HORIZON_URL}/transactions`, { method: "POST", body: formData });
        if (!res.ok) throw new Error(`Trustline failed: ${(await res.text()).slice(0, 200)}`);
        await new Promise((r) => setTimeout(r, 3000));
      }

      setStep("Building payment...");
      const amount = ceiling || cost;
      const tx = await buildGenericPayment(amount, key, deadlineSeconds);

      setStep("Approve in your wallet...");
      const signedXdr = await signTransaction(tx.unsigned_xdr);

      setStep("Submitting to Stellar...");
      const formData = new URLSearchParams({ tx: signedXdr });
      const horizonRes = await fetch(`${HORIZON_URL}/transactions`, { method: "POST", body: formData });
      if (!horizonRes.ok) throw new Error(`Payment failed: ${(await horizonRes.text()).slice(0, 200)}`);
      const txResult = await horizonRes.json();
      const txHash = txResult.hash || txResult.id || "";

      const paymentSig = btoa(JSON.stringify({ txHash, task_id: tx.task_id, success: true }));
      onPaymentComplete(paymentSig);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("cancel") || msg.includes("reject") || msg.includes("User")) {
        setError("Wallet signature cancelled. No payment was made.");
      } else {
        setError(msg);
      }
    } finally {
      setLoading(false);
      setStep("");
    }
  };

  return (
    <div className="card" style={{ maxWidth: 480 }}>
      <div className="section-label" style={{ marginBottom: 20 }}>// payment required</div>

      <div style={{ marginBottom: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 8 }}>
          <span style={{ fontSize: 12, color: "var(--muted)" }}>Estimated cost</span>
          <span style={{ fontSize: 22, fontWeight: 700, fontFamily: "var(--mono)", letterSpacing: -0.5 }}>{cost} USDC</span>
        </div>
        {ceiling && (
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 8 }}>
            <span style={{ fontSize: 12, color: "var(--muted)" }}>Budget lock</span>
            <span style={{ fontSize: 13, fontFamily: "var(--mono)", color: "var(--muted)" }}>{ceiling} USDC</span>
          </div>
        )}
        <div style={{ fontSize: 11, fontFamily: "var(--mono)", color: "var(--dim)", marginTop: 12 }}>
          {paymentInfo.network} / {paymentInfo.payTo.slice(0, 8)}...{paymentInfo.payTo.slice(-4)}
        </div>
      </div>

      <p style={{ fontSize: 12, color: "var(--muted)", lineHeight: 1.6, marginBottom: 20, padding: "12px", background: "var(--surface2)", borderRadius: "var(--r)" }}>
        Your funds stay locked in escrow until success, returned automatically on failure.
      </p>

      {error && (
        <p style={{ fontSize: 12, color: "#ef4444", marginBottom: 16, padding: "12px", background: "rgba(239,68,68,0.08)", borderRadius: "var(--r)" }}>
          {error}
        </p>
      )}

      {loading && step && (
        <div style={{ fontSize: 12, color: "var(--muted)", marginBottom: 16, display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ color: "var(--accent)", animation: "pulse 1.5s infinite" }}>*</span>
          {step}
        </div>
      )}

      <div style={{ display: "flex", gap: 10 }}>
        <button onClick={onCancel} className="btn-ghost" style={{ fontSize: 13, padding: "10px 20px" }} disabled={loading}>
          Cancel
        </button>
        <button onClick={handlePay} className="btn-primary" style={{ fontSize: 13, padding: "10px 20px", flex: 1 }} disabled={loading}>
          {loading ? step || "Processing..." : walletKey ? label : "Connect wallet and pay"}
        </button>
      </div>
    </div>
  );
}
