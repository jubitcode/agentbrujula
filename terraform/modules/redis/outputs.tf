output "id" {
  value = azurerm_redis_cache.main.id
}

output "host" {
  value = azurerm_redis_cache.main.hostname
}

output "port_tls" {
  value = azurerm_redis_cache.main.ssl_port
}

output "password_secret_name" {
  value = var.password_secret_name
}
