locals {
  name_prefix = "agent-canonical-${var.environment}"
  tags        = merge(var.tags, { environment = var.environment })
}

# Root resource group — holds everything except tfstate (which lives
# in its own RG provisioned by bootstrap/).
resource "azurerm_resource_group" "main" {
  name     = "rg-${local.name_prefix}"
  location = var.location
  tags     = local.tags
}

module "networking" {
  source = "./modules/networking"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

module "container_apps_env" {
  source = "./modules/container-apps-env"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  infrastructure_subnet_id = module.networking.aca_subnet_id
}

module "acr" {
  source = "./modules/acr"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

module "keyvault" {
  source = "./modules/keyvault"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
  tenant_id           = var.tenant_id

  operator_object_id = var.operator_object_id

  workload_identity_principal_id = module.identity.principal_id

  allowed_ip_ranges = var.keyvault_allowed_ip_ranges

  # Secret slots created with placeholder values; operator overwrites
  # via `az keyvault secret set` (run from the operator's machine)
  # postgres-admin-password + postgres-connection-string + redis-password
  # + session-token-secret are auto-populated by Terraform.
  # The rest are operator-seeded.
  secret_names = [
    "orchestrator-secret-key",
    "openrouter-api-key",
    "tavily-api-key",
    "auth-free-key",
    "auth-paid-key",
    "x402-facilitator-key",
  ]
}

# Session-token HMAC key — 64 hex chars of entropy, written to KV by Terraform.
# Container Apps revisions pick it up via the SESSION_TOKEN_SECRET env var below.
# Rotation: taint this resource, re-apply, then revision-restart ca-api → all
# users have to re-sign once (1 Freighter prompt, no data loss).
resource "random_password" "session_token_secret" {
  length  = 64
  special = false
  upper   = true
  lower   = true
  numeric = true
}

resource "azurerm_key_vault_secret" "session_token_secret" {
  name         = "session-token-secret"
  value        = random_password.session_token_secret.result
  key_vault_id = module.keyvault.id

  lifecycle {
    ignore_changes = [value]
  }
}

# User-assigned managed identity used by both container apps + jobs
# for: ACR pull, Key Vault read, Azure Storage access if added later.
module "identity" {
  source = "./modules/identity"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

# Identity → ACR pull
resource "azurerm_role_assignment" "mi_acr_pull" {
  scope                = module.acr.id
  role_definition_name = "AcrPull"
  principal_id         = module.identity.principal_id
}

module "postgres" {
  source = "./modules/postgres"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  delegated_subnet_id = module.networking.db_subnet_id
  private_dns_zone_id = module.networking.postgres_private_dns_zone_id

  admin_username = "agent_web3"
  # Password is generated once by Terraform, written to Key Vault.
  # Operator can rotate by deleting the random_password resource +
  # recreating; password change requires server restart.
  key_vault_id         = module.keyvault.id
  password_secret_name = "postgres-admin-password"

  database_name = "marketplace"
}

module "redis" {
  source = "./modules/redis"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  subnet_id           = module.networking.redis_subnet_id
  private_dns_zone_id = module.networking.redis_private_dns_zone_id

  key_vault_id         = module.keyvault.id
  password_secret_name = "redis-password"
}

module "container_app_api" {
  source = "./modules/container-app"

  name                          = "ca-api"
  container_apps_environment_id = module.container_apps_env.id
  resource_group_name           = azurerm_resource_group.main.name
  tags                          = local.tags

  identity_id      = module.identity.id
  acr_login_server = module.acr.login_server
  image            = "${module.acr.login_server}/api:${var.api_image_tag}"

  ingress_external = true
  target_port      = 5050
  cpu              = 0.5
  memory           = "1Gi"
  min_replicas     = 1
  max_replicas     = 3
  http_concurrency = 100

  liveness_probe_path  = "/health/stellar"
  readiness_probe_path = "/health/stellar"

  key_vault_id = module.keyvault.id

  # Key Vault references → injected as secret env vars.
  # DATABASE_URL is the full postgresql:// connection string the app reads
  # (task_store.py + hebbian/persistence.py both consume it as one env var).
  # Built in modules/postgres/ and stored as a KV secret to avoid app-side
  # assembly + URL-escape pain inside KV refs.
  kv_secret_refs = {
    "DATABASE_URL"            = module.postgres.connection_string_secret_name
    "ORCHESTRATOR_SECRET_KEY" = "orchestrator-secret-key"
    "OPENROUTER_API_KEY"      = "openrouter-api-key"
    "TAVILY_API_KEY"          = "tavily-api-key"
    "AUTH_FREE_KEY"           = "auth-free-key"
    "SESSION_TOKEN_SECRET"    = azurerm_key_vault_secret.session_token_secret.name
  }

  env_plain = {
    MCP_META_URL              = "http://${module.container_app_mcp_meta.fqdn}:9876"
    STELLAR_NETWORK           = var.stellar_network
    STELLAR_RPC_URL           = var.stellar_rpc_url
    STELLAR_HORIZON_URL       = var.stellar_horizon_url
    X402_ENABLED              = "true"
    X402_FACILITATOR_URL      = "https://x402.org/facilitator"
    X402_NETWORK              = "stellar:${var.stellar_network}"
    X402_USDC_ADDRESS         = var.x402_usdc_address
    X402_ESCROW_CONTRACT      = var.x402_escrow_contract
    TOOL_REGISTRY_CONTRACT_ID = var.tool_registry_contract_id
    ALLOWED_ORIGINS           = join(",", var.vercel_origin_patterns)
    # ca-api middleware rejects requests missing or mismatching this
    # header. Front Door sends it on every FD→origin call.
    AZURE_FDID = module.front_door.profile_resource_guid
  }

  # When the slowapi Redis backplane lands, add REDIS_URL here
  # (built same way as DATABASE_URL — modules/redis/ would write the full
  # rediss:// URL to KV).
  kv_secret_refs_for_built_strings = {}
}

module "container_app_mcp_meta" {
  source = "./modules/container-app"

  name                          = "ca-mcp-meta"
  container_apps_environment_id = module.container_apps_env.id
  resource_group_name           = azurerm_resource_group.main.name
  tags                          = local.tags

  identity_id      = module.identity.id
  acr_login_server = module.acr.login_server
  image            = "${module.acr.login_server}/mcp-meta:${var.mcp_meta_image_tag}"

  ingress_external = false # internal only — only ca-api reaches it
  target_port      = 9876
  cpu              = 1.0
  memory           = "2Gi"
  min_replicas     = 1
  max_replicas     = 1
  http_concurrency = 100

  liveness_probe_path  = "/health"
  readiness_probe_path = "/health"

  key_vault_id = module.keyvault.id

  kv_secret_refs = {
    "AUTH_FREE_KEY"      = "auth-free-key"
    "AUTH_PAID_KEY"      = "auth-paid-key"
    "OPENROUTER_API_KEY" = "openrouter-api-key"
    "TAVILY_API_KEY"     = "tavily-api-key"
  }

  env_plain = {
    AUTH_ENABLED  = "true"
    LLM_BASE_URL  = "https://openrouter.ai/api/v1"
    EMBEDDING_URL = "https://openrouter.ai/api/v1"
  }

  kv_secret_refs_for_built_strings = {}
}

# Cron jobs — reuse the api image
module "job_refund_sweeper" {
  source = "./modules/container-app-job"

  name                          = "caj-refund-sweeper"
  container_apps_environment_id = module.container_apps_env.id
  resource_group_name           = azurerm_resource_group.main.name
  location                      = azurerm_resource_group.main.location
  tags                          = local.tags

  identity_id     = module.identity.id
  image           = "${module.acr.login_server}/api:${var.api_image_tag}"
  command         = ["python", "-m", "research_agent.refund_sweeper"]
  cron_expression = "*/5 * * * *"
  replica_timeout = 240
  retry_limit     = 1
  cpu             = 0.5
  memory          = "1Gi"

  key_vault_id = module.keyvault.id

  kv_secret_refs = {
    "DATABASE_URL"            = module.postgres.connection_string_secret_name
    "ORCHESTRATOR_SECRET_KEY" = "orchestrator-secret-key"
  }

  env_plain = {
    STELLAR_NETWORK      = var.stellar_network
    STELLAR_RPC_URL      = var.stellar_rpc_url
    STELLAR_HORIZON_URL  = var.stellar_horizon_url
    X402_ESCROW_CONTRACT = var.x402_escrow_contract
  }
}

module "job_ttl_keeper" {
  source = "./modules/container-app-job"

  name                          = "caj-ttl-keeper"
  container_apps_environment_id = module.container_apps_env.id
  resource_group_name           = azurerm_resource_group.main.name
  location                      = azurerm_resource_group.main.location
  tags                          = local.tags

  identity_id     = module.identity.id
  image           = "${module.acr.login_server}/api:${var.api_image_tag}"
  command         = ["python", "-m", "research_agent.ttl_keeper"]
  cron_expression = "0 0 */14 * *"
  replica_timeout = 600
  retry_limit     = 2
  cpu             = 0.5
  memory          = "1Gi"

  key_vault_id = module.keyvault.id

  kv_secret_refs = {
    "ORCHESTRATOR_SECRET_KEY" = "orchestrator-secret-key"
  }

  env_plain = {
    STELLAR_NETWORK           = var.stellar_network
    STELLAR_RPC_URL           = var.stellar_rpc_url
    STELLAR_HORIZON_URL       = var.stellar_horizon_url
    X402_ESCROW_CONTRACT      = var.x402_escrow_contract
    TOOL_REGISTRY_CONTRACT_ID = var.tool_registry_contract_id
  }
}

module "front_door" {
  source = "./modules/front-door"

  resource_group_name = azurerm_resource_group.main.name
  name_prefix         = local.name_prefix
  tags                = local.tags

  origin_host_header = module.container_app_api.fqdn
  origin_hostname    = module.container_app_api.fqdn

  custom_domain    = var.custom_domain
  log_analytics_id = module.container_apps_env.log_analytics_workspace_id
}
