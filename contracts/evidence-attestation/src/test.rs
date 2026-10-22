#![cfg(test)]

use super::*;
use soroban_sdk::testutils::{Address as _, Ledger};
use soroban_sdk::{Address, BytesN, Env, String};

fn setup() -> (Env, EvidenceAttestationClient<'static>, Address, Address) {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(EvidenceAttestation, ());
    let client = EvidenceAttestationClient::new(&env, &contract_id);

    let attester = Address::generate(&env);
    let agent = Address::generate(&env);
    client.init(&attester);

    (env, client, attester, agent)
}

fn hash(env: &Env, fill: u8) -> BytesN<32> {
    BytesN::from_array(env, &[fill; 32])
}

// ---------------------------------------------------------------------------
// Writing an attestation
// ---------------------------------------------------------------------------

#[test]
fn records_a_passing_verdict() {
    let (env, client, _attester, agent) = setup();
    let task = String::from_str(&env, "task-1");
    let h = hash(&env, 0xAB);

    client.attest(&task, &agent, &h, &true);

    let record = client.get_attestation(&task).expect("attestation missing");
    assert_eq!(record.agent, agent);
    assert_eq!(record.evidence_hash, h);
    assert!(record.passed);
    assert!(client.has_passing_attestation(&task));
}

#[test]
fn records_a_failing_verdict() {
    // A failure is evidence too: it is what justifies a refund. It has to be
    // stored, not merely "not stored".
    let (env, client, _attester, agent) = setup();
    let task = String::from_str(&env, "task-2");
    let h = hash(&env, 0x11);

    client.attest(&task, &agent, &h, &false);

    let record = client.get_attestation(&task).expect("attestation missing");
    assert!(!record.passed);
    assert_eq!(record.evidence_hash, h);
    // The gate must refuse it just as firmly as a missing one.
    assert!(!client.has_passing_attestation(&task));
}

#[test]
fn stamps_the_ledger_time() {
    let (env, client, _attester, agent) = setup();
    env.ledger().with_mut(|l| l.timestamp = 1_792_600_000);

    let task = String::from_str(&env, "task-3");
    client.attest(&task, &agent, &hash(&env, 1), &true);

    assert_eq!(
        client.get_attestation(&task).unwrap().attested_at,
        1_792_600_000
    );
}

// ---------------------------------------------------------------------------
// Append-only
// ---------------------------------------------------------------------------

#[test]
fn refuses_a_second_attestation_for_the_same_task() {
    // The property the whole design rests on. If a verdict could be rewritten,
    // a settlement could be justified after the fact by whoever moved the
    // money — and replaying the chain would prove nothing.
    let (env, client, _attester, agent) = setup();
    let task = String::from_str(&env, "task-4");

    client.attest(&task, &agent, &hash(&env, 1), &false);

    let second = client.try_attest(&task, &agent, &hash(&env, 2), &true);
    assert_eq!(second, Err(Ok(Error::AlreadyAttested)));

    // The original verdict stands.
    let record = client.get_attestation(&task).unwrap();
    assert!(!record.passed);
    assert_eq!(record.evidence_hash, hash(&env, 1));
}

#[test]
fn different_tasks_are_independent() {
    let (env, client, _attester, agent) = setup();
    let pass = String::from_str(&env, "task-pass");
    let fail = String::from_str(&env, "task-fail");

    client.attest(&pass, &agent, &hash(&env, 1), &true);
    client.attest(&fail, &agent, &hash(&env, 2), &false);

    assert!(client.has_passing_attestation(&pass));
    assert!(!client.has_passing_attestation(&fail));
}

// ---------------------------------------------------------------------------
// Reading what was never written
// ---------------------------------------------------------------------------

#[test]
fn an_unattested_task_reads_as_none_not_a_panic() {
    // `usdc-escrow::get_escrow` ends in `.expect()` and traps when the entry is
    // absent, which forces every caller to parse a trap to learn "not found".
    // This returns None instead.
    let (env, client, _attester, _agent) = setup();
    let task = String::from_str(&env, "never-attested");

    assert!(client.get_attestation(&task).is_none());
    assert!(!client.has_passing_attestation(&task));
    assert!(!client.matches_evidence(&task, &hash(&env, 1)));
}

// ---------------------------------------------------------------------------
// Evidence matching — what a verifier page asks
// ---------------------------------------------------------------------------

#[test]
fn matches_only_the_attested_hash() {
    let (env, client, _attester, agent) = setup();
    let task = String::from_str(&env, "task-5");
    let real = hash(&env, 0xAA);
    let altered = hash(&env, 0xAB);

    client.attest(&task, &agent, &real, &true);

    assert!(client.matches_evidence(&task, &real));
    // A package altered after the verdict must not match. This is the whole
    // point of anchoring the hash.
    assert!(!client.matches_evidence(&task, &altered));
}

#[test]
fn matches_evidence_on_a_failing_attestation_too() {
    // Hash matching answers "is this the package that was judged?", which is a
    // separate question from "did it pass". A reviewer checking why a refund
    // happened needs the first one to work.
    let (env, client, _attester, agent) = setup();
    let task = String::from_str(&env, "task-6");
    let h = hash(&env, 0x55);

    client.attest(&task, &agent, &h, &false);

    assert!(client.matches_evidence(&task, &h));
    assert!(!client.has_passing_attestation(&task));
}

// ---------------------------------------------------------------------------
// Initialisation
// ---------------------------------------------------------------------------

#[test]
fn init_can_only_happen_once() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(EvidenceAttestation, ());
    let client = EvidenceAttestationClient::new(&env, &contract_id);

    let first = Address::generate(&env);
    let usurper = Address::generate(&env);

    client.init(&first);
    assert_eq!(client.try_init(&usurper), Err(Ok(Error::AlreadyInitialised)));
    assert_eq!(client.attester(), Some(first));
}

#[test]
fn attesting_before_init_is_refused() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(EvidenceAttestation, ());
    let client = EvidenceAttestationClient::new(&env, &contract_id);

    let agent = Address::generate(&env);
    let task = String::from_str(&env, "task-7");

    assert_eq!(
        client.try_attest(&task, &agent, &hash(&env, 1), &true),
        Err(Ok(Error::NotInitialised))
    );
}

// ---------------------------------------------------------------------------
// Authorisation
// ---------------------------------------------------------------------------

#[test]
fn attesting_requires_the_attester_s_authorisation() {
    // Without mock_all_auths the call must fail: an attestation anyone could
    // write would gate nothing at all.
    let env = Env::default();
    let contract_id = env.register(EvidenceAttestation, ());
    let client = EvidenceAttestationClient::new(&env, &contract_id);

    let attester = Address::generate(&env);
    let agent = Address::generate(&env);
    client.init(&attester);

    let task = String::from_str(&env, "task-8");
    let result = client.try_attest(&task, &agent, &hash(&env, 1), &true);
    assert!(result.is_err(), "unauthorised attestation was accepted");
}
