resource "random_string" "suffix" {
  length  = 4
  upper   = false
  special = false
  numeric = true
}

resource "azurerm_redis_cache" "main" {
  name                = "redis-${var.name_prefix}-${random_string.suffix.result}"
  resource_group_name = var.resource_group_name
  location            = var.location
  tags                = var.tags

  capacity             = var.capacity
  family               = "C"
  sku_name             = var.sku_name
  non_ssl_port_enabled = false
  minimum_tls_version  = "1.2"

  public_network_access_enabled = false

  redis_configuration {
    maxmemory_policy = "allkeys-lru"
  }
}

# Private endpoint so ca-api can reach Redis over the VNet.
resource "azurerm_private_endpoint" "redis" {
  name                = "pe-redis-${var.name_prefix}"
  resource_group_name = var.resource_group_name
  location            = var.location
  subnet_id           = var.subnet_id
  tags                = var.tags

  private_service_connection {
    name                           = "psc-redis"
    private_connection_resource_id = azurerm_redis_cache.main.id
    is_manual_connection           = false
    subresource_names              = ["redisCache"]
  }

  private_dns_zone_group {
    name                 = "redis-dns"
    private_dns_zone_ids = [var.private_dns_zone_id]
  }
}

# Stash the primary access key in Key Vault
resource "azurerm_key_vault_secret" "redis_password" {
  name         = var.password_secret_name
  value        = azurerm_redis_cache.main.primary_access_key
  key_vault_id = var.key_vault_id

  lifecycle {
    ignore_changes = [value]
  }
}
