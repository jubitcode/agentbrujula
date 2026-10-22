#![no_std]
//! Evidence attestation — the quality gate that decides whether money moves.
//!
//! `compression-compliance` answers "is the output the right size?". A size is
//! not a judgement about the work: an agent can return a well-proportioned
//! answer that is wrong. This contract records the judgement itself — the hash
//! of an evidence package (the tests that ran, their results, the deliverable)
//! together with a verdict — so that a settlement can be gated on evidence
//! rather than on a heuristic.
//!
//! Three properties it is built to have:
//!
//! 1. **Append-only.** An attestation for a task can be written once. Nobody,
//!    the attester included, can revise a verdict after the fact, so the chain
//!    can be replayed to check that any past settlement was justified.
//! 2. **Authorised.** Only the configured attester may write. An attestation
//!    anyone could forge would gate nothing.
//! 3. **Readable.** `has_passing_attestation` is a cheap predicate so a gate —
//!    on-chain or off — can ask the question without decoding a struct.
//!
//! The evidence package itself stays off-chain. Only its SHA-256 is anchored
//! here, which is what makes the package tamper-evident: re-hash it and compare.

use soroban_sdk::{
    contract, contracterror, contractimpl, contracttype, symbol_short, Address, BytesN, Env,
    String, Symbol,
};

/// Who may write attestations. Set once at init.
const ATTESTER: Symbol = symbol_short!("ATTESTER");

#[contracttype]
#[derive(Clone, Debug)]
pub struct Attestation {
    /// The agent the work is attributed to.
    ///
    /// Stored as an Address — the agent's payout key, which is what
    /// `tool-registry` already holds. The 8004 identity registry binds an
    /// identity to exactly this address, so when that lands the link is a
    /// lookup, not a migration.
    pub agent: Address,
    /// SHA-256 of the evidence package. 32 bytes, fixed width.
    pub evidence_hash: BytesN<32>,
    /// The verdict. False is a real outcome, not an absence — a failing
    /// attestation is the record that justifies a refund.
    pub passed: bool,
    /// Ledger timestamp when it was written.
    pub attested_at: u64,
}

#[contracttype]
pub enum DataKey {
    Attestation(String),
}

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum Error {
    /// `init` was called on an already-initialised contract.
    AlreadyInitialised = 1,
    /// A contract function was called before `init`.
    NotInitialised = 2,
    /// This task already has an attestation. Attestations are append-only.
    AlreadyAttested = 3,
}

#[contract]
pub struct EvidenceAttestation;

#[contractimpl]
impl EvidenceAttestation {
    /// Set the account allowed to write attestations. Callable once.
    pub fn init(env: Env, attester: Address) -> Result<(), Error> {
        if env.storage().instance().has(&ATTESTER) {
            return Err(Error::AlreadyInitialised);
        }
        env.storage().instance().set(&ATTESTER, &attester);
        Ok(())
    }

    /// Record the verdict on a task's evidence package.
    ///
    /// Requires the attester's authorisation, and refuses a second attestation
    /// for the same task — the record is what a settlement is justified by, so
    /// it must not be rewritable after the money has moved.
    ///
    /// Emits `("attest", task_id)` either way: a failing verdict is evidence
    /// too, and a refund needs it as much as a payout needs a passing one.
    pub fn attest(
        env: Env,
        task_id: String,
        agent: Address,
        evidence_hash: BytesN<32>,
        passed: bool,
    ) -> Result<(), Error> {
        let attester: Address = env
            .storage()
            .instance()
            .get(&ATTESTER)
            .ok_or(Error::NotInitialised)?;
        attester.require_auth();

        let key = DataKey::Attestation(task_id.clone());
        if env.storage().persistent().has(&key) {
            return Err(Error::AlreadyAttested);
        }

        let record = Attestation {
            agent: agent.clone(),
            evidence_hash: evidence_hash.clone(),
            passed,
            attested_at: env.ledger().timestamp(),
        };

        env.storage().persistent().set(&key, &record);
        // Outlive a 30-day engagement comfortably; the keeper extends it
        // further for anything that has to last.
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);

        env.events().publish(
            (symbol_short!("attest"), task_id),
            (agent, evidence_hash, passed, record.attested_at),
        );

        Ok(())
    }

    /// The attestation for a task, or None.
    ///
    /// Returns an Option rather than panicking: "no attestation yet" is an
    /// ordinary answer a caller has to handle, not an error condition.
    pub fn get_attestation(env: Env, task_id: String) -> Option<Attestation> {
        env.storage()
            .persistent()
            .get(&DataKey::Attestation(task_id))
    }

    /// Whether this task has an attestation AND it passed.
    ///
    /// The predicate a settlement gate needs. Deliberately collapses "no
    /// attestation" and "attested as failed" into false: both mean the same
    /// thing to a gate — there is no evidence that justifies releasing money.
    pub fn has_passing_attestation(env: Env, task_id: String) -> bool {
        env.storage()
            .persistent()
            .get::<DataKey, Attestation>(&DataKey::Attestation(task_id))
            .map(|a| a.passed)
            .unwrap_or(false)
    }

    /// Whether `evidence_hash` is the one attested for this task.
    ///
    /// This is how a verifier page checks an uploaded package: re-hash the ZIP
    /// and ask the chain whether that is the hash the verdict was given on. A
    /// package that was altered after the fact fails here.
    pub fn matches_evidence(env: Env, task_id: String, evidence_hash: BytesN<32>) -> bool {
        env.storage()
            .persistent()
            .get::<DataKey, Attestation>(&DataKey::Attestation(task_id))
            .map(|a| a.evidence_hash == evidence_hash)
            .unwrap_or(false)
    }

    /// The configured attester.
    pub fn attester(env: Env) -> Option<Address> {
        env.storage().instance().get(&ATTESTER)
    }
}

#[cfg(test)]
mod test;
