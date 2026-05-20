locals {
  tags = merge(var.tags, { environment = var.environment })
}

module "container_app_api" {
  source = "../modules/container-app"

  name                          = "ca-api"
  container_apps_environment_id = local.p.container_apps_environment_id
  resource_group_name           = local.p.resource_group_name
  tags                          = local.tags

  identity_id      = local.p.managed_identity_id
  acr_login_server = local.p.acr_login_server
  image            = "${local.p.acr_login_server}/api:${var.api_image_tag}"

  ingress_external = true
  target_port      = 5050
  cpu              = 0.5
  memory           = "1Gi"
  min_replicas     = 1
  max_replicas     = 3
  http_concurrency = 100

  liveness_probe_path  = "/health/stellar"
  readiness_probe_path = "/health/stellar"

  key_vault_id = local.p.key_vault_id

  kv_secret_refs = {
    "DATABASE_URL"            = local.p.postgres_connection_secret_name
    "ORCHESTRATOR_SECRET_KEY" = "orchestrator-secret-key"
    "OPENROUTER_API_KEY"      = "openrouter-api-key"
    "TAVILY_API_KEY"          = "tavily-api-key"
    "AUTH_FREE_KEY"           = "auth-free-key"
    "SESSION_TOKEN_SECRET"    = local.p.session_token_secret_name
  }

  env_plain = merge(
    {
      MCP_META_URL              = "https://${module.container_app_mcp_meta.fqdn}"
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
    },
    var.deploy_front_door ? {
      AZURE_FDID = module.front_door[0].profile_resource_guid
    } : {},
  )

  kv_secret_refs_for_built_strings = {}
}

module "container_app_mcp_meta" {
  source = "../modules/container-app"

  name                          = "ca-mcp-meta"
  container_apps_environment_id = local.p.container_apps_environment_id
  resource_group_name           = local.p.resource_group_name
  tags                          = local.tags

  identity_id      = local.p.managed_identity_id
  acr_login_server = local.p.acr_login_server
  image            = "${local.p.acr_login_server}/mcp-meta:${var.mcp_meta_image_tag}"

  ingress_external = false
  target_port      = 9876
  cpu              = 1.0
  memory           = "2Gi"
  min_replicas     = 1
  max_replicas     = 1
  http_concurrency = 100

  liveness_probe_path  = "/health"
  readiness_probe_path = "/health"

  key_vault_id = local.p.key_vault_id

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

module "job_refund_sweeper" {
  source = "../modules/container-app-job"

  name                          = "caj-refund-sweeper"
  container_apps_environment_id = local.p.container_apps_environment_id
  resource_group_name           = local.p.resource_group_name
  location                      = local.p.location
  tags                          = local.tags

  identity_id     = local.p.managed_identity_id
  image           = "${local.p.acr_login_server}/api:${var.api_image_tag}"
  command         = ["python", "-m", "research_agent.refund_sweeper"]
  cron_expression = "*/5 * * * *"
  replica_timeout = 240
  retry_limit     = 1
  cpu             = 0.5
  memory          = "1Gi"

  key_vault_id = local.p.key_vault_id

  kv_secret_refs = {
    "DATABASE_URL"            = local.p.postgres_connection_secret_name
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
  source = "../modules/container-app-job"

  name                          = "caj-ttl-keeper"
  container_apps_environment_id = local.p.container_apps_environment_id
  resource_group_name           = local.p.resource_group_name
  location                      = local.p.location
  tags                          = local.tags

  identity_id     = local.p.managed_identity_id
  image           = "${local.p.acr_login_server}/api:${var.api_image_tag}"
  command         = ["python", "-m", "research_agent.ttl_keeper"]
  cron_expression = "0 0 */14 * *"
  replica_timeout = 600
  retry_limit     = 2
  cpu             = 0.5
  memory          = "1Gi"

  key_vault_id = local.p.key_vault_id

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
  source = "../modules/front-door"

  count = var.deploy_front_door ? 1 : 0

  resource_group_name = local.p.resource_group_name
  name_prefix         = "agent-canonical-${var.environment}"
  tags                = local.tags

  origin_host_header = module.container_app_api.fqdn
  origin_hostname    = module.container_app_api.fqdn

  custom_domain    = var.custom_domain
  log_analytics_id = local.p.log_analytics_workspace_id
}
