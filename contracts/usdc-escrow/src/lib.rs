#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracttype, symbol_short, token, Address, Env, String, Symbol, Vec,
};

#[contracttype]
#[derive(Clone, PartialEq, Debug)]
pub enum EscrowStatus {
    Created,
    Released,
    Refunded,
    Settled,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct Split {
    pub addr: Address,
    pub amount: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct EscrowEntry {
    pub client: Address,
    pub agent: Address,
    pub amount: i128,
    pub deadline: u64,
    pub status: EscrowStatus,
}

const OWNER: Symbol = symbol_short!("OWNER");
const ORCH: Symbol = symbol_short!("ORCH");
const TOKEN: Symbol = symbol_short!("TOKEN");
const PAUSED: Symbol = symbol_short!("PAUSED");

#[contracttype]
pub enum DataKey {
    Escrow(String),
}

#[contract]
pub struct UsdcEscrow;

#[contractimpl]
impl UsdcEscrow {
    /// Initialize the contract. Can only be called once.
    /// - owner: can pause/unpause and change orchestrator
    /// - orchestrator: can call release_to_agent
    /// - usdc_token: address of the USDC token contract (SEP-41)
    pub fn init(env: Env, owner: Address, orchestrator: Address, usdc_token: Address) {
        if env.storage().instance().has(&OWNER) {
            panic!("already initialized");
        }
        owner.require_auth();
        env.storage().instance().set(&OWNER, &owner);
        env.storage().instance().set(&ORCH, &orchestrator);
        env.storage().instance().set(&TOKEN, &usdc_token);
        env.storage().instance().set(&PAUSED, &false);
        env.storage().instance().extend_ttl(100_000, 200_000);
    }

    /// Create an escrow: transfers USDC from client to this contract.
    /// Requires client auth. Panics if paused, amount<=0, deadline too soon, or duplicate task_id.
    pub fn create_escrow(
        env: Env,
        client: Address,
        agent: Address,
        amount: i128,
        deadline: u64,
        task_id: String,
    ) {
        Self::require_not_paused(&env);
        client.require_auth();

        if amount <= 0 {
            panic!("amount must be positive");
        }

        let now = env.ledger().timestamp();
        if deadline <= now {
            panic!("deadline must be in the future");
        }
        if deadline < now + 10 {
            panic!("deadline too soon (min 10 seconds)");
        }

        let key = DataKey::Escrow(task_id.clone());
        if env.storage().persistent().has(&key) {
            panic!("task_id already exists");
        }

        // Transfer USDC from client to this contract
        let token_addr: Address = env.storage().instance().get(&TOKEN).unwrap();
        let token_client = token::Client::new(&env, &token_addr);
        token_client.transfer(&client, &env.current_contract_address(), &amount);

        let entry = EscrowEntry {
            client,
            agent,
            amount,
            deadline,
            status: EscrowStatus::Created,
        };
        env.storage().persistent().set(&key, &entry);
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);
        env.storage().instance().extend_ttl(100_000, 200_000);

        env.events().publish(
            (symbol_short!("escrow"), symbol_short!("create")),
            (task_id, amount, deadline),
        );
    }

    /// Release USDC to the agent. Only callable by the orchestrator.
    /// Panics if not orchestrator, if escrow not in Created status, or if paused.
    pub fn release_to_agent(env: Env, task_id: String) {
        Self::require_not_paused(&env);
        let orch: Address = env.storage().instance().get(&ORCH).unwrap();
        orch.require_auth();

        let key = DataKey::Escrow(task_id.clone());
        let mut entry: EscrowEntry = env
            .storage()
            .persistent()
            .get(&key)
            .expect("escrow not found");

        if entry.status != EscrowStatus::Created {
            panic!("escrow already settled");
        }

        // Effects before interactions: flip status and persist BEFORE the
        // external token transfer so a reentrant call (defense-in-depth
        // against any future hostile token contract) hits the status guard.
        let amount = entry.amount;
        let agent = entry.agent.clone();
        entry.status = EscrowStatus::Released;
        env.storage().persistent().set(&key, &entry);
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);

        // Interaction
        let token_addr: Address = env.storage().instance().get(&TOKEN).unwrap();
        let token_client = token::Client::new(&env, &token_addr);
        token_client.transfer(&env.current_contract_address(), &agent, &amount);

        env.events().publish(
            (symbol_short!("escrow"), symbol_short!("release")),
            (task_id, amount),
        );
    }

    /// Refund USDC to the client. Only callable after deadline has passed.
    /// Anyone can call this (permissionless after deadline — client gets their money back).
    pub fn refund_to_client(env: Env, task_id: String) {
        Self::require_not_paused(&env);

        let key = DataKey::Escrow(task_id.clone());
        let mut entry: EscrowEntry = env
            .storage()
            .persistent()
            .get(&key)
            .expect("escrow not found");

        if entry.status != EscrowStatus::Created {
            panic!("escrow already settled");
        }

        let now = env.ledger().timestamp();
        if now < entry.deadline {
            panic!("deadline not reached");
        }

        // Effects before interactions (see release_to_agent for rationale).
        let amount = entry.amount;
        let client = entry.client.clone();
        entry.status = EscrowStatus::Refunded;
        env.storage().persistent().set(&key, &entry);
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);

        // Interaction
        let token_addr: Address = env.storage().instance().get(&TOKEN).unwrap();
        let token_client = token::Client::new(&env, &token_addr);
        token_client.transfer(&env.current_contract_address(), &client, &amount);

        env.events().publish(
            (symbol_short!("escrow"), symbol_short!("refund")),
            (task_id, amount),
        );
    }

    /// Settle escrow: pay agent_amount to agent, refund remainder to client.
    /// Atomic on-chain split — one transaction, no intermediate state.
    /// Only callable by orchestrator. Escrow must be in Created status.
    pub fn settle(env: Env, task_id: String, agent_amount: i128) {
        Self::require_not_paused(&env);
        let orch: Address = env.storage().instance().get(&ORCH).unwrap();
        orch.require_auth();

        let key = DataKey::Escrow(task_id.clone());
        let mut entry: EscrowEntry = env
            .storage()
            .persistent()
            .get(&key)
            .expect("escrow not found");

        if entry.status != EscrowStatus::Created {
            panic!("escrow already settled");
        }
        if agent_amount < 0 || agent_amount > entry.amount {
            panic!("invalid agent amount");
        }

        // Effects before interactions: snapshot the addresses + compute refund,
        // flip status, persist — THEN transfer.
        let agent = entry.agent.clone();
        let client = entry.client.clone();
        let refund = entry.amount - agent_amount;
        entry.status = EscrowStatus::Settled;
        env.storage().persistent().set(&key, &entry);
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);

        // Interactions
        let token_addr: Address = env.storage().instance().get(&TOKEN).unwrap();
        let token_client = token::Client::new(&env, &token_addr);
        let contract = env.current_contract_address();
        if agent_amount > 0 {
            token_client.transfer(&contract, &agent, &agent_amount);
        }
        if refund > 0 {
            token_client.transfer(&contract, &client, &refund);
        }

        env.events().publish(
            (symbol_short!("escrow"), symbol_short!("settle")),
            (task_id, agent_amount, refund),
        );
    }

    /// Settle escrow with multiple agents (team hire).
    /// Each split goes to its address. Remainder refunded to client.
    /// Atomic — all transfers in one transaction.
    /// Only callable by orchestrator.
    pub fn settle_team(env: Env, task_id: String, splits: Vec<Split>) {
        Self::require_not_paused(&env);
        let orch: Address = env.storage().instance().get(&ORCH).unwrap();
        orch.require_auth();

        let key = DataKey::Escrow(task_id.clone());
        let mut entry: EscrowEntry = env
            .storage()
            .persistent()
            .get(&key)
            .expect("escrow not found");

        if entry.status != EscrowStatus::Created {
            panic!("escrow already settled");
        }

        // Validate first (no external calls yet).
        let mut total_agent: i128 = 0;
        for i in 0..splits.len() {
            let split = splits.get(i as u32).unwrap();
            if split.amount < 0 {
                panic!("split amount must be non-negative");
            }
            total_agent += split.amount;
        }
        if total_agent > entry.amount {
            panic!("splits exceed escrow amount");
        }

        // Effects before interactions: snapshot client + refund, flip status,
        // persist — THEN do the N token transfers.
        let client = entry.client.clone();
        let refund = entry.amount - total_agent;
        entry.status = EscrowStatus::Settled;
        env.storage().persistent().set(&key, &entry);
        env.storage().persistent().extend_ttl(&key, 100_000, 200_000);

        // Interactions
        let token_addr: Address = env.storage().instance().get(&TOKEN).unwrap();
        let token_client = token::Client::new(&env, &token_addr);
        let contract = env.current_contract_address();
        for i in 0..splits.len() {
            let split = splits.get(i as u32).unwrap();
            if split.amount > 0 {
                token_client.transfer(&contract, &split.addr, &split.amount);
            }
        }
        if refund > 0 {
            token_client.transfer(&contract, &client, &refund);
        }

        env.events().publish(
            (symbol_short!("escrow"), symbol_short!("team")),
            (task_id, total_agent, refund),
        );
    }

    /// Read escrow state (free, no auth needed).
    pub fn get_escrow(env: Env, task_id: String) -> EscrowEntry {
        let key = DataKey::Escrow(task_id);
        env.storage()
            .persistent()
            .get(&key)
            .expect("escrow not found")
    }

    // --- Admin functions ---

    /// Pause the contract. Only owner.
    pub fn pause(env: Env) {
        let owner: Address = env.storage().instance().get(&OWNER).unwrap();
        owner.require_auth();
        env.storage().instance().set(&PAUSED, &true);
    }

    /// Unpause the contract. Only owner.
    pub fn unpause(env: Env) {
        let owner: Address = env.storage().instance().get(&OWNER).unwrap();
        owner.require_auth();
        env.storage().instance().set(&PAUSED, &false);
    }

    /// Check if paused (read-only).
    pub fn is_paused(env: Env) -> bool {
        env.storage()
            .instance()
            .get(&PAUSED)
            .unwrap_or(false)
    }

    // --- Internal helpers ---

    fn require_not_paused(env: &Env) {
        let paused: bool = env.storage().instance().get(&PAUSED).unwrap_or(false);
        if paused {
            panic!("contract is paused");
        }
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::{
        testutils::{Address as _, Ledger, LedgerInfo},
        token::{StellarAssetClient, TokenClient},
        Env, String,
    };

    fn setup() -> (Env, Address, Address, Address, Address, Address) {
        let env = Env::default();
        env.mock_all_auths();

        // Set ledger timestamp
        env.ledger().set(LedgerInfo {
            timestamp: 1000,
            protocol_version: 23,
            sequence_number: 100,
            network_id: [0u8; 32],
            base_reserve: 10,
            min_temp_entry_ttl: 100,
            min_persistent_entry_ttl: 100,
            max_entry_ttl: 10000,
        });

        let owner = Address::generate(&env);
        let orchestrator = Address::generate(&env);
        let client_addr = Address::generate(&env);
        let agent = Address::generate(&env);

        // Deploy a test token (USDC mock)
        let token_admin = Address::generate(&env);
        let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
        let token_addr = token_contract.address();
        let token_admin_client = StellarAssetClient::new(&env, &token_addr);

        // Mint USDC to client
        token_admin_client.mint(&client_addr, &10_000_000_000); // 10,000 USDC (7 decimals)

        // Deploy escrow contract
        let escrow_id = env.register(UsdcEscrow, ());
        let escrow_client = UsdcEscrowClient::new(&env, &escrow_id);
        escrow_client.init(&owner, &orchestrator, &token_addr);

        (env, escrow_id, client_addr, agent, orchestrator, token_addr)
    }

    fn task_id(env: &Env, name: &str) -> String {
        String::from_str(env, name)
    }

    #[test]
    fn test_create_escrow_locks_usdc() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000; // 0.1 USDC
        let tid = task_id(&env, "task-1");

        let client_before = token.balance(&client);
        escrow.create_escrow(&client, &agent, &amount, &2000, &tid);
        let client_after = token.balance(&client);

        // Client balance decreased
        assert_eq!(client_before - client_after, amount);

        // Contract holds the funds
        let contract_balance = token.balance(&escrow_id);
        assert_eq!(contract_balance, amount);

        // Escrow entry is correct
        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.status, EscrowStatus::Created);
        assert_eq!(entry.amount, amount);
    }

    #[test]
    fn test_release_to_agent_pays_agent() {
        let (env, escrow_id, client, agent, orch, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000;
        let tid = task_id(&env, "task-2");
        escrow.create_escrow(&client, &agent, &amount, &2000, &tid);

        let agent_before = token.balance(&agent);
        escrow.release_to_agent(&tid);
        let agent_after = token.balance(&agent);

        assert_eq!(agent_after - agent_before, amount);

        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.status, EscrowStatus::Released);
    }

    #[test]
    fn test_refund_after_deadline() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000;
        let tid = task_id(&env, "task-3");
        escrow.create_escrow(&client, &agent, &amount, &1500, &tid);

        // Advance time past deadline
        env.ledger().set(LedgerInfo {
            timestamp: 2000,
            protocol_version: 23,
            sequence_number: 200,
            network_id: [0u8; 32],
            base_reserve: 10,
            min_temp_entry_ttl: 100,
            min_persistent_entry_ttl: 100,
            max_entry_ttl: 10000,
        });

        let client_before = token.balance(&client);
        escrow.refund_to_client(&tid);
        let client_after = token.balance(&client);

        assert_eq!(client_after - client_before, amount);

        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.status, EscrowStatus::Refunded);
    }

    #[test]
    fn test_get_escrow_returns_correct_state() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);

        let tid = task_id(&env, "task-4");
        escrow.create_escrow(&client, &agent, &500_000, &2000, &tid);

        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.client, client);
        assert_eq!(entry.agent, agent);
        assert_eq!(entry.amount, 500_000);
        assert_eq!(entry.deadline, 2000);
        assert_eq!(entry.status, EscrowStatus::Created);
    }

    #[test]
    #[should_panic(expected = "amount must be positive")]
    fn test_create_escrow_zero_amount_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-1");
        escrow.create_escrow(&client, &agent, &0, &2000, &tid);
    }

    #[test]
    #[should_panic(expected = "deadline must be in the future")]
    fn test_create_escrow_past_deadline_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-2");
        // Ledger is at 1000, deadline at 500
        escrow.create_escrow(&client, &agent, &1000, &500, &tid);
    }

    #[test]
    #[should_panic(expected = "deadline too soon")]
    fn test_create_escrow_deadline_too_soon_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-3");
        // Ledger at 1000, deadline at 1005 (< 10s away)
        escrow.create_escrow(&client, &agent, &1000, &1005, &tid);
    }

    #[test]
    #[should_panic(expected = "escrow already settled")]
    fn test_double_release_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-4");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        escrow.release_to_agent(&tid);
        // Second release should panic
        escrow.release_to_agent(&tid);
    }

    #[test]
    #[should_panic(expected = "escrow already settled")]
    fn test_refund_after_release_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-5");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        escrow.release_to_agent(&tid);

        // Advance past deadline
        env.ledger().set(LedgerInfo {
            timestamp: 3000,
            protocol_version: 23,
            sequence_number: 300,
            network_id: [0u8; 32],
            base_reserve: 10,
            min_temp_entry_ttl: 100,
            min_persistent_entry_ttl: 100,
            max_entry_ttl: 10000,
        });

        // Should panic — already released
        escrow.refund_to_client(&tid);
    }

    #[test]
    #[should_panic(expected = "deadline not reached")]
    fn test_refund_before_deadline_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "bad-6");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        // Ledger still at 1000, deadline at 2000
        escrow.refund_to_client(&tid);
    }

    #[test]
    #[should_panic(expected = "task_id already exists")]
    fn test_duplicate_task_id_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "dup-1");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        // Same task_id again
        escrow.create_escrow(&client, &agent, &500_000, &2000, &tid);
    }

    #[test]
    #[should_panic(expected = "already initialized")]
    fn test_double_init_panics() {
        let (env, escrow_id, _, _, orch, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let owner2 = Address::generate(&env);
        // Already initialized in setup()
        escrow.init(&owner2, &orch, &token_addr);
    }

    #[test]
    #[should_panic(expected = "contract is paused")]
    fn test_create_escrow_when_paused_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        escrow.pause();
        let tid = task_id(&env, "paused-1");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
    }

    #[test]
    fn test_unpause_restores_operations() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        escrow.pause();
        assert!(escrow.is_paused());
        escrow.unpause();
        assert!(!escrow.is_paused());

        // Should work after unpause
        let tid = task_id(&env, "unpause-1");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.status, EscrowStatus::Created);
    }

    #[test]
    #[should_panic(expected = "contract is paused")]
    fn test_release_when_paused_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "paused-2");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        escrow.pause();
        escrow.release_to_agent(&tid);
    }

    #[test]
    #[should_panic(expected = "contract is paused")]
    fn test_refund_when_paused_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "paused-3");
        escrow.create_escrow(&client, &agent, &1_000_000, &1500, &tid);

        env.ledger().set(LedgerInfo {
            timestamp: 2000,
            protocol_version: 23,
            sequence_number: 200,
            network_id: [0u8; 32],
            base_reserve: 10,
            min_temp_entry_ttl: 100,
            min_persistent_entry_ttl: 100,
            max_entry_ttl: 10000,
        });

        escrow.pause();
        escrow.refund_to_client(&tid);
    }

    #[test]
    fn test_multiple_escrows_isolated() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let tid1 = task_id(&env, "multi-1");
        let tid2 = task_id(&env, "multi-2");

        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid1);
        escrow.create_escrow(&client, &agent, &2_000_000, &2000, &tid2);

        // Release only task 1
        escrow.release_to_agent(&tid1);

        let e1 = escrow.get_escrow(&tid1);
        let e2 = escrow.get_escrow(&tid2);
        assert_eq!(e1.status, EscrowStatus::Released);
        assert_eq!(e2.status, EscrowStatus::Created);

        // Contract still holds task 2 funds
        let contract_balance = token.balance(&escrow_id);
        assert_eq!(contract_balance, 2_000_000);
    }

    #[test]
    fn test_settle_pays_agent_and_refunds_client() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let budget: i128 = 2_000_000; // 0.2 USDC budget
        let actual_cost: i128 = 1_500_000; // 0.15 USDC actual
        let tid = task_id(&env, "settle-1");
        escrow.create_escrow(&client, &agent, &budget, &2000, &tid);

        let agent_before = token.balance(&agent);
        let client_before = token.balance(&client);

        escrow.settle(&tid, &actual_cost);

        let agent_after = token.balance(&agent);
        let client_after = token.balance(&client);

        // Agent gets actual cost
        assert_eq!(agent_after - agent_before, actual_cost);
        // Client gets refund
        assert_eq!(client_after - client_before, budget - actual_cost);

        let entry = escrow.get_escrow(&tid);
        assert_eq!(entry.status, EscrowStatus::Settled);

        // Contract holds nothing
        let contract_balance = token.balance(&escrow_id);
        assert_eq!(contract_balance, 0);
    }

    #[test]
    fn test_settle_zero_refund() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000;
        let tid = task_id(&env, "settle-2");
        escrow.create_escrow(&client, &agent, &amount, &2000, &tid);

        let agent_before = token.balance(&agent);
        escrow.settle(&tid, &amount); // Full amount to agent, zero refund

        assert_eq!(token.balance(&agent) - agent_before, amount);
        assert_eq!(escrow.get_escrow(&tid).status, EscrowStatus::Settled);
    }

    #[test]
    fn test_settle_zero_agent_amount() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000;
        let tid = task_id(&env, "settle-3");
        escrow.create_escrow(&client, &agent, &amount, &2000, &tid);

        let client_before = token.balance(&client);
        escrow.settle(&tid, &0); // Full refund to client

        assert_eq!(token.balance(&client) - client_before, amount);
        assert_eq!(escrow.get_escrow(&tid).status, EscrowStatus::Settled);
    }

    #[test]
    #[should_panic(expected = "invalid agent amount")]
    fn test_settle_overpay_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "settle-4");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        escrow.settle(&tid, &2_000_000); // More than escrowed
    }

    #[test]
    #[should_panic(expected = "escrow already settled")]
    fn test_settle_twice_panics() {
        let (env, escrow_id, client, agent, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let tid = task_id(&env, "settle-5");
        escrow.create_escrow(&client, &agent, &1_000_000, &2000, &tid);
        escrow.settle(&tid, &500_000);
        escrow.settle(&tid, &500_000); // Already settled
    }

    #[test]
    fn test_settle_team_two_agents() {
        let (env, escrow_id, client, _, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let agent_a = Address::generate(&env);
        let agent_b = Address::generate(&env);

        let budget: i128 = 3_000_000;
        let tid = task_id(&env, "team-settle-1");
        // Use agent_a as the "primary" agent in escrow entry
        escrow.create_escrow(&client, &agent_a, &budget, &2000, &tid);

        let a_before = token.balance(&agent_a);
        let b_before = token.balance(&agent_b);
        let client_before = token.balance(&client);

        let mut splits = Vec::new(&env);
        splits.push_back(Split { addr: agent_a.clone(), amount: 1_000_000 });
        splits.push_back(Split { addr: agent_b.clone(), amount: 1_500_000 });

        escrow.settle_team(&tid, &splits);

        assert_eq!(token.balance(&agent_a) - a_before, 1_000_000);
        assert_eq!(token.balance(&agent_b) - b_before, 1_500_000);
        assert_eq!(token.balance(&client) - client_before, 500_000); // refund

        assert_eq!(escrow.get_escrow(&tid).status, EscrowStatus::Settled);
        assert_eq!(token.balance(&escrow_id), 0);
    }

    #[test]
    fn test_settle_team_three_agents_zero_refund() {
        let (env, escrow_id, client, _, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let agent_a = Address::generate(&env);
        let agent_b = Address::generate(&env);
        let agent_c = Address::generate(&env);

        let budget: i128 = 3_000_000;
        let tid = task_id(&env, "team-settle-2");
        escrow.create_escrow(&client, &agent_a, &budget, &2000, &tid);

        let mut splits = Vec::new(&env);
        splits.push_back(Split { addr: agent_a.clone(), amount: 1_000_000 });
        splits.push_back(Split { addr: agent_b.clone(), amount: 1_000_000 });
        splits.push_back(Split { addr: agent_c.clone(), amount: 1_000_000 });

        escrow.settle_team(&tid, &splits);

        assert_eq!(token.balance(&agent_a), 1_000_000);
        assert_eq!(token.balance(&agent_b), 1_000_000);
        assert_eq!(token.balance(&agent_c), 1_000_000);
        assert_eq!(escrow.get_escrow(&tid).status, EscrowStatus::Settled);
    }

    #[test]
    #[should_panic(expected = "splits exceed escrow amount")]
    fn test_settle_team_exceeds_budget_panics() {
        let (env, escrow_id, client, _, _, _) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);

        let agent_a = Address::generate(&env);
        let agent_b = Address::generate(&env);

        let tid = task_id(&env, "team-settle-3");
        escrow.create_escrow(&client, &agent_a, &1_000_000, &2000, &tid);

        let mut splits = Vec::new(&env);
        splits.push_back(Split { addr: agent_a.clone(), amount: 800_000 });
        splits.push_back(Split { addr: agent_b.clone(), amount: 800_000 }); // total 1.6M > 1M

        escrow.settle_team(&tid, &splits);
    }

    #[test]
    fn test_settle_team_empty_splits_refunds_all() {
        let (env, escrow_id, client, agent, _, token_addr) = setup();
        let escrow = UsdcEscrowClient::new(&env, &escrow_id);
        let token = TokenClient::new(&env, &token_addr);

        let amount: i128 = 1_000_000;
        let tid = task_id(&env, "team-settle-4");
        escrow.create_escrow(&client, &agent, &amount, &2000, &tid);

        let client_before = token.balance(&client);
        let splits = Vec::new(&env); // empty
        escrow.settle_team(&tid, &splits);

        assert_eq!(token.balance(&client) - client_before, amount); // full refund
        assert_eq!(escrow.get_escrow(&tid).status, EscrowStatus::Settled);
    }
}
