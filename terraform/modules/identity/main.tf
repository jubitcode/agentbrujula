# Single user-assigned managed identity shared by all Container Apps
# + Jobs. Gets AcrPull + Key Vault Secrets User downstream.
resource "azurerm_user_assigned_identity" "main" {
  name                = "mi-${var.name_prefix}"
  resource_group_name = var.resource_group_name
  location            = var.location
  tags                = var.tags
}
