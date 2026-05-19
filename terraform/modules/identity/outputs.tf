output "id" {
  value = azurerm_user_assigned_identity.main.id
}

output "principal_id" {
  value = azurerm_user_assigned_identity.main.principal_id
}

output "client_id" {
  value = azurerm_user_assigned_identity.main.client_id
}

output "tenant_id" {
  value = azurerm_user_assigned_identity.main.tenant_id
}
