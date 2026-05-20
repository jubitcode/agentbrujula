locals {
  name_prefix = "agent-canonical-${var.environment}"
  tags        = merge(var.tags, { environment = var.environment })
}

resource "azurerm_resource_group" "main" {
  name     = "rg-${local.name_prefix}"
  location = var.location
  tags     = local.tags
}

module "networking" {
  source = "../modules/networking"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

module "identity" {
  source = "../modules/identity"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

module "acr" {
  source = "../modules/acr"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
}

resource "azurerm_role_assignment" "mi_acr_pull" {
  scope                = module.acr.id
  role_definition_name = "AcrPull"
  principal_id         = module.identity.principal_id
}

module "keyvault" {
  source = "../modules/keyvault"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags
  tenant_id           = var.tenant_id

  operator_object_id             = var.operator_object_id
  workload_identity_principal_id = module.identity.principal_id

  allowed_ip_ranges  = var.keyvault_allowed_ip_ranges
  allowed_subnet_ids = [module.networking.aca_subnet_id]

  secret_names = [
    "orchestrator-secret-key",
    "openrouter-api-key",
    "tavily-api-key",
    "auth-free-key",
    "auth-paid-key",
    "x402-facilitator-key",
  ]
}

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

module "postgres" {
  source = "../modules/postgres"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  delegated_subnet_id = module.networking.db_subnet_id
  private_dns_zone_id = module.networking.postgres_private_dns_zone_id

  admin_username       = "agent_web3"
  key_vault_id         = module.keyvault.id
  password_secret_name = "postgres-admin-password"
  database_name        = "marketplace"
}

module "redis" {
  source = "../modules/redis"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  subnet_id           = module.networking.redis_subnet_id
  private_dns_zone_id = module.networking.redis_private_dns_zone_id

  key_vault_id         = module.keyvault.id
  password_secret_name = "redis-password"
}

module "container_apps_env" {
  source = "../modules/container-apps-env"

  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  name_prefix         = local.name_prefix
  tags                = local.tags

  infrastructure_subnet_id = module.networking.aca_subnet_id
}
