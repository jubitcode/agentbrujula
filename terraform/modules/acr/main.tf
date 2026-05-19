resource "random_string" "suffix" {
  length  = 6
  upper   = false
  special = false
  numeric = true
}

# ACR names must be globally unique 5-50 alphanumeric.
resource "azurerm_container_registry" "main" {
  name                = "acr${replace(var.name_prefix, "-", "")}${random_string.suffix.result}"
  resource_group_name = var.resource_group_name
  location            = var.location
  sku                 = var.sku
  admin_enabled       = false # Managed identity auth only — no admin user
  tags                = var.tags

  public_network_access_enabled = true # Basic SKU doesn't support private endpoint; lock down at Standard+
}
