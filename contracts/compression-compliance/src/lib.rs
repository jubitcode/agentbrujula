#![no_std]
use soroban_sdk::{contract, contractimpl, symbol_short, Env, String};

#[contract]
pub struct CompressionCompliance;

#[contractimpl]
impl CompressionCompliance {
    /// Verify that output tokens don't exceed a compression ratio of the input.
    ///
    /// - `task_id`: unique task identifier
    /// - `input_tokens`: number of tokens in the input/context
    /// - `output_tokens`: number of tokens in the agent's output
    /// - `max_ratio_pct`: maximum allowed ratio as percentage (e.g. 50 = output must be <= 50% of input)
    ///
    /// Returns true if compliant. Emits an on-chain attestation event either way.
    pub fn verify_compression(
        env: Env,
        task_id: String,
        input_tokens: u32,
        output_tokens: u32,
        max_ratio_pct: u32,
    ) -> bool {
        if input_tokens == 0 {
            env.events().publish(
                (symbol_short!("comply"), task_id),
                (input_tokens, output_tokens, 0u32, false),
            );
            return false;
        }

        // ratio = (output_tokens * 100) / input_tokens
        let ratio = (output_tokens as u64 * 100) / (input_tokens as u64);
        let passed = ratio <= max_ratio_pct as u64;

        env.events().publish(
            (symbol_short!("comply"), task_id),
            (input_tokens, output_tokens, ratio as u32, passed),
        );

        passed
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::Env;

    #[test]
    fn test_compliant() {
        let env = Env::default();
        let contract_id = env.register_contract(None, CompressionCompliance);
        let client = CompressionComplianceClient::new(&env, &contract_id);

        let task = String::from_str(&env, "test-1");
        let result = client.verify_compression(&task, &1000, &200, &50);
        assert!(result); // 20% <= 50%
    }

    #[test]
    fn test_non_compliant() {
        let env = Env::default();
        let contract_id = env.register_contract(None, CompressionCompliance);
        let client = CompressionComplianceClient::new(&env, &contract_id);

        let task = String::from_str(&env, "test-2");
        let result = client.verify_compression(&task, &1000, &900, &50);
        assert!(!result); // 90% > 50%
    }

    #[test]
    fn test_exact_boundary() {
        let env = Env::default();
        let contract_id = env.register_contract(None, CompressionCompliance);
        let client = CompressionComplianceClient::new(&env, &contract_id);

        let task = String::from_str(&env, "test-3");
        let result = client.verify_compression(&task, &1000, &500, &50);
        assert!(result); // 50% <= 50%
    }

    #[test]
    fn test_zero_input() {
        let env = Env::default();
        let contract_id = env.register_contract(None, CompressionCompliance);
        let client = CompressionComplianceClient::new(&env, &contract_id);

        let task = String::from_str(&env, "test-4");
        let result = client.verify_compression(&task, &0, &100, &50);
        assert!(!result); // zero input = non-compliant
    }
}
