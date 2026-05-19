resource "azurerm_log_analytics_workspace" "main" {
  name                = "log-${var.name_prefix}"
  resource_group_name = var.resource_group_name
  location            = var.location
  sku                 = "PerGB2018"
  retention_in_days   = var.log_retention_days
  tags                = var.tags
}

resource "azurerm_container_app_environment" "main" {
  name                = "cae-${var.name_prefix}"
  resource_group_name = var.resource_group_name
  location            = var.location
  tags                = var.tags

  log_analytics_workspace_id = azurerm_log_analytics_workspace.main.id

  infrastructure_subnet_id       = var.infrastructure_subnet_id
  internal_load_balancer_enabled = false # we want external ingress for ca-api
  zone_redundancy_enabled        = false

  workload_profile {
    name                  = "Consumption"
    workload_profile_type = "Consumption"
  }
}
