#![no_std]
use soroban_sdk::{
    contract, contractimpl, contracttype, symbol_short, token, Address, Env, Map, String, Symbol,
    Vec,
};

const TOOLS: Symbol = symbol_short!("TOOLS");
const TASK_LOG: Symbol = symbol_short!("TASKLOG");
const ADMIN: Symbol = symbol_short!("ADMIN");
const REP_ASSET: Symbol = symbol_short!("REPASSET");

#[contracttype]
#[derive(Clone)]
pub struct ToolEntry {
    pub agent: Address,
    pub handle: String,
    pub url: String, // HTTP endpoint the orchestrator calls after payment
    pub capabilities: Vec<String>,
    pub model_tier: String, // "HAIKU" | "SONNET" | "OPUS"
    pub rate_stroops: i128, // cost in stroops (1 XLM = 10_000_000)
    pub max_tokens: u32,
    pub active: bool,
    pub regis: u64,
}

#[contracttype]
#[derive(Clone)]
pub struct TaskClaim {
    pub task_id: String,
    pub tool: String,
    pub agent: Address,
    pub requester: Address,
    pub claimed_at: u64,
    pub deadline: u64,
    pub status: TaskStatus,
}

#[contracttype]
#[derive(Clone, PartialEq)]
pub enum TaskStatus {
    Pending,
    Running,
    Complete,
    Timeout,
    Disputed,
}

#[contract]
pub struct ToolRegistry;

#[contractimpl]
impl ToolRegistry {
    pub fn init(env: Env, admin: Address, rep_asset: Address) {
        if env.storage().instance().has(&ADMIN) {
            panic!("already initialized");
        }
        admin.require_auth();
        env.storage().instance().set(&ADMIN, &admin);
        env.storage().instance().set(&REP_ASSET, &rep_asset);
        env.storage().instance().extend_ttl(100_000, 200_000);
    }

    pub fn register_tool(
        env: Env,
        agent: Address,
        handle: String,
        url: String,
        capabilities: Vec<String>,
        model_tier: String,
        rate_stroops: i128,
        max_tokens: u32,
    ) {
        agent.require_auth();

        let mut tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .unwrap_or(Map::new(&env));

        if tools.contains_key(handle.clone()) {
            panic!("handle already registered");
        }

        tools.set(
            handle.clone(),
            ToolEntry {
                agent: agent.clone(),
                handle: handle.clone(),
                url,
                capabilities,
                model_tier,
                rate_stroops,
                max_tokens,
                active: true,
                regis: env.ledger().timestamp(),
            },
        );

        env.storage().persistent().set(&TOOLS, &tools);
        env.storage()
            .persistent()
            .extend_ttl(&TOOLS, 100_000, 200_000);
        env.storage().instance().extend_ttl(100_000, 200_000);
        env.events()
            .publish((symbol_short!("regis"), agent), handle);
    }

    /// Update mutable fields on a tool entry the caller owns.
    ///
    /// Identity fields (`agent`, `handle`, `regis`) and `active` are NOT
    /// touched — use `deactivate_tool` to flip `active`. This lets agents
    /// change their URL, capabilities, model tier, rate, and max-token
    /// limit without requiring a full registry redeploy (the previous
    /// behavior, where `register_tool` panicked on duplicate handles, made
    /// the URL effectively permanent).
    ///
    /// Only the original registering agent can update its own entry —
    /// `entry.agent == agent` is enforced. Mirrors `deactivate_tool`'s
    /// auth shape.
    pub fn update_tool(
        env: Env,
        agent: Address,
        handle: String,
        url: String,
        capabilities: Vec<String>,
        model_tier: String,
        rate_stroops: i128,
        max_tokens: u32,
    ) {
        agent.require_auth();

        let mut tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .unwrap_or(Map::new(&env));

        let mut entry = tools.get(handle.clone()).expect("tool not found");
        if entry.agent != agent {
            panic!("not your tool");
        }

        // Mutable fields only; identity (agent, handle, regis) + active preserved.
        entry.url = url;
        entry.capabilities = capabilities;
        entry.model_tier = model_tier;
        entry.rate_stroops = rate_stroops;
        entry.max_tokens = max_tokens;

        tools.set(handle.clone(), entry);
        env.storage().persistent().set(&TOOLS, &tools);
        env.storage()
            .persistent()
            .extend_ttl(&TOOLS, 100_000, 200_000);
        env.storage().instance().extend_ttl(100_000, 200_000);
        env.events()
            .publish((symbol_short!("update"), agent), handle);
    }

    pub fn deactivate_tool(env: Env, agent: Address, handle: String) {
        agent.require_auth();

        let mut tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .unwrap_or(Map::new(&env));

        let mut entry = tools.get(handle.clone()).expect("tool not found");
        if entry.agent != agent {
            panic!("not your tool");
        }

        entry.active = false;
        tools.set(handle.clone(), entry);
        env.storage().persistent().set(&TOOLS, &tools);
        env.storage()
            .persistent()
            .extend_ttl(&TOOLS, 100_000, 200_000);
    }

    pub fn list_active(env: Env) -> Vec<String> {
        let tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .unwrap_or(Map::new(&env));

        let mut active = Vec::new(&env);
        for handle in tools.keys() {
            if let Some(entry) = tools.get(handle.clone()) {
                if entry.active {
                    active.push_back(handle);
                }
            }
        }
        active
    }

    pub fn list_active_full(env: Env) -> Vec<ToolEntry> {
        let tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .unwrap_or(Map::new(&env));

        let mut active = Vec::new(&env);
        for handle in tools.keys() {
            if let Some(entry) = tools.get(handle.clone()) {
                if entry.active {
                    active.push_back(entry);
                }
            }
        }
        active
    }

    pub fn get_tool(env: Env, handle: String) -> ToolEntry {
        let tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .expect("no tools registered");
        tools.get(handle).expect("tool not found")
    }

    pub fn get_rep(env: Env, agent: Address) -> i128 {
        let rep_asset: Address = env
            .storage()
            .instance()
            .get(&REP_ASSET)
            .expect("rep asset not set");
        let token_client = token::TokenClient::new(&env, &rep_asset);
        token_client.balance(&agent)
    }

    pub fn claim_task(
        env: Env,
        requester: Address,
        task_id: String,
        handle: String,
        deadline: u64,
    ) {
        requester.require_auth();

        let tools: Map<String, ToolEntry> = env
            .storage()
            .persistent()
            .get(&TOOLS)
            .expect("no tools registered");

        let entry = tools.get(handle.clone()).expect("tool not found");
        if !entry.active {
            panic!("tool is offline");
        }
        if deadline <= env.ledger().timestamp() + 10 {
            panic!("deadline too soon");
        }

        let mut log: Map<String, TaskClaim> = env
            .storage()
            .persistent()
            .get(&TASK_LOG)
            .unwrap_or(Map::new(&env));

        if log.contains_key(task_id.clone()) {
            panic!("task_id already exists");
        }

        log.set(
            task_id.clone(),
            TaskClaim {
                task_id: task_id.clone(),
                tool: handle,
                agent: entry.agent,
                requester: requester.clone(),
                claimed_at: env.ledger().timestamp(),
                deadline,
                status: TaskStatus::Pending,
            },
        );

        env.storage().persistent().set(&TASK_LOG, &log);
        env.storage()
            .persistent()
            .extend_ttl(&TASK_LOG, 100_000, 200_000);
        env.events()
            .publish((symbol_short!("claimed"), requester), task_id);
    }

    pub fn resolve_task(env: Env, agent: Address, task_id: String, success: bool) {
        agent.require_auth();

        let mut log: Map<String, TaskClaim> =
            env.storage().persistent().get(&TASK_LOG).expect("no tasks");

        let mut claim = log.get(task_id.clone()).expect("task not found");
        if claim.agent != agent {
            panic!("not your task");
        }
        if claim.status != TaskStatus::Running && claim.status != TaskStatus::Pending {
            panic!("task already resolved");
        }

        if env.ledger().timestamp() > claim.deadline {
            claim.status = TaskStatus::Timeout;
            log.set(task_id.clone(), claim.clone());
            env.storage().persistent().set(&TASK_LOG, &log);
            env.storage()
                .persistent()
                .extend_ttl(&TASK_LOG, 100_000, 200_000);
            env.events()
                .publish((symbol_short!("timeout"), claim.agent), task_id);
            return;
        }

        claim.status = if success {
            TaskStatus::Complete
        } else {
            TaskStatus::Disputed
        };
        log.set(task_id.clone(), claim.clone());
        env.storage().persistent().set(&TASK_LOG, &log);
        env.storage()
            .persistent()
            .extend_ttl(&TASK_LOG, 100_000, 200_000);

        if success {
            Self::mint_rep(&env, &claim.agent, 10);
        }

        env.events()
            .publish((symbol_short!("resolved"), claim.agent), (task_id, success));
    }

    pub fn get_task(env: Env, task_id: String) -> TaskClaim {
        let log: Map<String, TaskClaim> =
            env.storage().persistent().get(&TASK_LOG).expect("no tasks");
        log.get(task_id).expect("task not found")
    }

    fn mint_rep(env: &Env, agent: &Address, amount: i128) {
        let rep_asset: Address = env
            .storage()
            .instance()
            .get(&REP_ASSET)
            .expect("rep asset not set");
        let token_client = token::StellarAssetClient::new(env, &rep_asset);
        token_client.mint(agent, &amount);
    }

    pub fn slash_rep(env: Env, agent: Address, amount: i128) {
        let admin: Address = env
            .storage()
            .instance()
            .get(&ADMIN)
            .expect("not initialized");
        admin.require_auth();

        let rep_asset: Address = env
            .storage()
            .instance()
            .get(&REP_ASSET)
            .expect("rep asset not set");
        let token_client = token::StellarAssetClient::new(&env, &rep_asset);
        token_client.clawback(&agent, &amount);
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::{testutils::Address as _, Env, String};

    fn setup_env() -> (Env, Address, Address) {
        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);
        let rep_asset = env
            .register_stellar_asset_contract_v2(admin.clone())
            .address();

        (env, admin, rep_asset)
    }

    #[test]
    fn test_init_requires_admin_auth() {
        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);
        let rep_asset = env
            .register_stellar_asset_contract_v2(admin.clone())
            .address();

        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);

        client.init(&admin, &rep_asset);

        // Verify auth was required for admin
        let auths = env.auths();
        assert!(!auths.is_empty(), "init() must require admin auth");
    }

    #[test]
    #[should_panic(expected = "already initialized")]
    fn test_init_cannot_reinitialize() {
        let (env, admin, rep_asset) = setup_env();

        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);

        client.init(&admin, &rep_asset);
        // Second init should panic
        client.init(&admin, &rep_asset);
    }

    #[test]
    fn test_register_and_list_active() {
        let (env, admin, rep_asset) = setup_env();

        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);

        client.init(&admin, &rep_asset);

        let agent = Address::generate(&env);
        let handle = String::from_str(&env, "test-agent");
        let url = String::from_str(&env, "http://localhost:3001/invoke");
        let mut caps = Vec::new(&env);
        caps.push_back(String::from_str(&env, "research"));
        let tier = String::from_str(&env, "SONNET");

        client.register_tool(&agent, &handle, &url, &caps, &tier, &100_000, &4096);

        let active = client.list_active();
        assert_eq!(active.len(), 1);
    }


    fn register_sample(env: &Env, client: &ToolRegistryClient, agent: &Address, handle: &str) {
        let mut caps = Vec::new(env);
        caps.push_back(String::from_str(env, "research"));
        client.register_tool(
            agent,
            &String::from_str(env, handle),
            &String::from_str(env, "http://old.example.com/invoke"),
            &caps,
            &String::from_str(env, "SONNET"),
            &100_000,
            &4096,
        );
    }

    #[test]
    fn test_update_tool_changes_mutable_fields() {
        let (env, admin, rep_asset) = setup_env();
        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);
        client.init(&admin, &rep_asset);

        let agent = Address::generate(&env);
        register_sample(&env, &client, &agent, "test-agent");

        let mut new_caps = Vec::new(&env);
        new_caps.push_back(String::from_str(&env, "research"));
        new_caps.push_back(String::from_str(&env, "summarize"));
        client.update_tool(
            &agent,
            &String::from_str(&env, "test-agent"),
            &String::from_str(&env, "https://new.example.com/agents/test-agent"),
            &new_caps,
            &String::from_str(&env, "OPUS"),
            &500_000,
            &8192,
        );

        let entry = client.get_tool(&String::from_str(&env, "test-agent"));
        assert_eq!(
            entry.url,
            String::from_str(&env, "https://new.example.com/agents/test-agent")
        );
        assert_eq!(entry.capabilities.len(), 2);
        assert_eq!(entry.model_tier, String::from_str(&env, "OPUS"));
        assert_eq!(entry.rate_stroops, 500_000);
        assert_eq!(entry.max_tokens, 8192);
    }

    #[test]
    fn test_update_tool_preserves_identity_fields() {
        let (env, admin, rep_asset) = setup_env();
        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);
        client.init(&admin, &rep_asset);

        let agent = Address::generate(&env);
        register_sample(&env, &client, &agent, "test-agent");
        let before = client.get_tool(&String::from_str(&env, "test-agent"));

        let mut caps = Vec::new(&env);
        caps.push_back(String::from_str(&env, "x"));
        client.update_tool(
            &agent,
            &String::from_str(&env, "test-agent"),
            &String::from_str(&env, "http://new.example.com"),
            &caps,
            &String::from_str(&env, "HAIKU"),
            &1,
            &1,
        );

        let after = client.get_tool(&String::from_str(&env, "test-agent"));
        // Identity fields unchanged.
        assert_eq!(after.agent, before.agent, "agent identity must not change");
        assert_eq!(after.handle, before.handle, "handle must not change");
        assert_eq!(
            after.regis, before.regis,
            "registration timestamp must not change"
        );
        // active is also preserved — use deactivate_tool to flip it.
        assert_eq!(after.active, before.active);
    }

    #[test]
    #[should_panic(expected = "not your tool")]
    fn test_update_tool_rejects_non_owner() {
        let (env, admin, rep_asset) = setup_env();
        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);
        client.init(&admin, &rep_asset);

        let owner = Address::generate(&env);
        let attacker = Address::generate(&env);
        register_sample(&env, &client, &owner, "test-agent");

        let mut caps = Vec::new(&env);
        caps.push_back(String::from_str(&env, "hijack"));
        client.update_tool(
            &attacker,
            &String::from_str(&env, "test-agent"),
            &String::from_str(&env, "http://attacker.example.com"),
            &caps,
            &String::from_str(&env, "OPUS"),
            &1,
            &1,
        );
    }

    #[test]
    #[should_panic(expected = "tool not found")]
    fn test_update_tool_rejects_unknown_handle() {
        let (env, admin, rep_asset) = setup_env();
        let contract_id = env.register_contract(None, ToolRegistry);
        let client = ToolRegistryClient::new(&env, &contract_id);
        client.init(&admin, &rep_asset);

        let agent = Address::generate(&env);
        let caps = Vec::new(&env);
        client.update_tool(
            &agent,
            &String::from_str(&env, "never-registered"),
            &String::from_str(&env, "http://nope.example.com"),
            &caps,
            &String::from_str(&env, "HAIKU"),
            &1,
            &1,
        );
    }
}
