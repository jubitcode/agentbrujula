.PHONY: agent-test api-dev contracts-build contracts-deploy contracts-deploy-mainnet mcp-build verify web-dev web-test web-build

NETWORK ?= testnet
SOURCE  ?= orchestrator

ifeq ($(NETWORK),testnet)
  RPC_URL            := https://soroban-testnet.stellar.org
  NETWORK_PASSPHRASE := Test SDF Network ; September 2015
else ifeq ($(NETWORK),mainnet)
  RPC_URL            := https://soroban.stellar.org
  NETWORK_PASSPHRASE := Public Global Stellar Network ; September 2015
else
  $(error Unknown NETWORK='$(NETWORK)'. Use NETWORK=testnet or NETWORK=mainnet)
endif

agent-test:
	cd agent && uv run pytest

# Run every suite and regenerate VERIFICATION.md with the real result,
# failures included. Anyone who clones the repo can reproduce this.
verify:
	./scripts/verify.sh

api-dev:
	cd agent && PYTHONPATH=../api uv run uvicorn server:app --reload --port 5050

contracts-build:
	cd contracts && stellar contract build

contracts-deploy: contracts-build
	@echo "Deploying contracts to $(NETWORK) (source=$(SOURCE))..."
	@echo "Tool Registry:"
	cd contracts && stellar contract deploy \
		--wasm target/wasm32v1-none/release/tool_registry.wasm \
		--rpc-url "$(RPC_URL)" \
		--network-passphrase "$(NETWORK_PASSPHRASE)" \
		--source $(SOURCE)
	@echo "USDC Escrow:"
	cd contracts && stellar contract deploy \
		--wasm target/wasm32v1-none/release/usdc_escrow.wasm \
		--rpc-url "$(RPC_URL)" \
		--network-passphrase "$(NETWORK_PASSPHRASE)" \
		--source $(SOURCE)
	@echo "Compression Compliance:"
	cd contracts && stellar contract deploy \
		--wasm target/wasm32v1-none/release/compression_compliance.wasm \
		--rpc-url "$(RPC_URL)" \
		--network-passphrase "$(NETWORK_PASSPHRASE)" \
		--source $(SOURCE)
	@echo "Remember to init contracts and update .env with new IDs"
	@echo "See contracts/DEPLOYMENTS.md for details"

# Mainnet target — refuses to use the testnet 'orchestrator' alias.
# Caller must explicitly pass SOURCE=<mainnet-key-alias>, e.g.
#   make contracts-deploy-mainnet SOURCE=mainnet-multisig
contracts-deploy-mainnet:
	@if [ "$(SOURCE)" = "orchestrator" ]; then \
		echo "ERROR: refusing to deploy to mainnet with the testnet 'orchestrator' alias."; \
		echo "Pass SOURCE=<mainnet-key-alias> explicitly."; \
		exit 1; \
	fi
	@$(MAKE) contracts-deploy NETWORK=mainnet SOURCE=$(SOURCE)

mcp-build:
	cd mcp-meta && go build ./cmd/mcp-meta

web-dev:
	cd mcp-meta/web && npm run dev

web-test:
	cd mcp-meta/web && npm test

web-build:
	cd mcp-meta/web && npm run build
