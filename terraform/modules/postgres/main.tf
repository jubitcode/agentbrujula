resource "random_string" "name_suffix" {
  length  = 4
  upper   = false
  special = false
  numeric = true
}

# Generate password once. Stored in Key Vault.
# Rotation: taint this resource, apply, then update Postgres admin
# password via `az postgres flexible-server update`.
resource "random_password" "admin" {
  length      = 32
  special     = true
  min_lower   = 2
  min_upper   = 2
  min_numeric = 2
  min_special = 1

  # PG admin password cannot contain certain chars
  override_special = "!#$%&*+-=?@_"
}

resource "azurerm_key_vault_secret" "admin_password" {
  name         = var.password_secret_name
  value        = random_password.admin.result
  key_vault_id = var.key_vault_id

  # First-write only — Terraform stops fighting manual rotations
  lifecycle {
    ignore_changes = [value]
  }
}

# Full DATABASE_URL the app reads (research_agent.task_store + hebbian.persistence
# both call os.environ["DATABASE_URL"]). Built here so no app-side assembly is
# needed and rotation = update one secret + restart revision.
resource "azurerm_key_vault_secret" "connection_string" {
  name         = var.connection_string_secret_name
  # urlencode the password so URL-reserved chars (%, !, &, =, etc. that are
  # in override_special) don't break psycopg2's DSN parser.
  value        = "postgresql://${var.admin_username}:${urlencode(random_password.admin.result)}@${azurerm_postgresql_flexible_server.main.fqdn}:5432/${var.database_name}?sslmode=require"
  key_vault_id = var.key_vault_id

  lifecycle {
    ignore_changes = [value]
  }

  depends_on = [azurerm_postgresql_flexible_server_database.main]
}

resource "azurerm_postgresql_flexible_server" "main" {
  name                = "pg-${var.name_prefix}-${random_string.name_suffix.result}"
  resource_group_name = var.resource_group_name
  location            = var.location
  tags                = var.tags

  version  = var.postgres_version
  sku_name = var.sku_name

  storage_mb                   = var.storage_mb
  backup_retention_days        = var.backup_retention_days
  geo_redundant_backup_enabled = false

  administrator_login    = var.admin_username
  administrator_password = random_password.admin.result

  delegated_subnet_id = var.delegated_subnet_id
  private_dns_zone_id = var.private_dns_zone_id

  public_network_access_enabled = false
  zone                          = "1"

  authentication {
    password_auth_enabled         = true
    active_directory_auth_enabled = false
  }

  lifecycle {
    # azurerm sometimes plans a no-op zone change; ignore to avoid noise
    ignore_changes = [zone, high_availability]
  }
}

resource "azurerm_postgresql_flexible_server_database" "main" {
  name      = var.database_name
  server_id = azurerm_postgresql_flexible_server.main.id
  collation = "en_US.utf8"
  charset   = "UTF8"
}

# Enable pgcrypto + uuid-ossp via server param
resource "azurerm_postgresql_flexible_server_configuration" "extensions" {
  name      = "azure.extensions"
  server_id = azurerm_postgresql_flexible_server.main.id
  value     = "PGCRYPTO,UUID-OSSP"
}
