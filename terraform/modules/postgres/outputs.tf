output "id" {
  value = azurerm_postgresql_flexible_server.main.id
}

output "fqdn" {
  value = azurerm_postgresql_flexible_server.main.fqdn
}

output "admin_username" {
  value = var.admin_username
}

output "database_name" {
  value = var.database_name
}

output "password_secret_name" {
  value = var.password_secret_name
}

output "connection_string_secret_name" {
  value = var.connection_string_secret_name
}
