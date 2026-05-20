resource "random_string" "suffix" {
  length  = 4
  upper   = false
  special = false
  numeric = true
}

# Key Vault names must be globally unique 3-24 chars.
resource "azurerm_key_vault" "main" {
  name                = "kv-${substr(var.name_prefix, 0, 14)}-${random_string.suffix.result}"
  resource_group_name = var.resource_group_name
  location            = var.location
  tenant_id           = var.tenant_id
  tags                = var.tags

  sku_name                   = "standard"
  rbac_authorization_enabled = true
  soft_delete_retention_days = 90
  purge_protection_enabled   = true

  public_network_access_enabled = true

  network_acls {
    default_action             = length(var.allowed_ip_ranges) == 0 && length(var.allowed_subnet_ids) == 0 ? "Allow" : "Deny"
    bypass                     = "AzureServices"
    ip_rules                   = var.allowed_ip_ranges
    virtual_network_subnet_ids = var.allowed_subnet_ids
  }
}

# Container Apps managed identity reads secrets
resource "azurerm_role_assignment" "workload_secrets_user" {
  scope                = azurerm_key_vault.main.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = var.workload_identity_principal_id
}

# Operator (you) seeds and rotates secrets
resource "azurerm_role_assignment" "operator_secrets_officer" {
  scope                = azurerm_key_vault.main.id
  role_definition_name = "Key Vault Secrets Officer"
  principal_id         = var.operator_object_id
}

# Pre-create secret slots with placeholder values so the Container
# Apps can boot (they'll fail health checks until real values are set
# but Terraform won't get stuck on a missing secret reference).
# `ignore_changes` on value means Terraform won't fight manual
# `az keyvault secret set` updates.
resource "azurerm_key_vault_secret" "placeholders" {
  for_each = toset(var.secret_names)

  name         = each.key
  value        = "PLACEHOLDER_SET_VIA_AZ_KEYVAULT_SECRET_SET"
  key_vault_id = azurerm_key_vault.main.id

  lifecycle {
    ignore_changes = [value]
  }

  depends_on = [azurerm_role_assignment.operator_secrets_officer]
}
