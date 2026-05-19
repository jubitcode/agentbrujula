output "vnet_id" {
  value = azurerm_virtual_network.main.id
}

output "aca_subnet_id" {
  value = azurerm_subnet.aca.id
}

output "db_subnet_id" {
  value = azurerm_subnet.db.id
}

output "redis_subnet_id" {
  value = azurerm_subnet.redis.id
}

output "postgres_private_dns_zone_id" {
  value = azurerm_private_dns_zone.postgres.id
}

output "redis_private_dns_zone_id" {
  value = azurerm_private_dns_zone.redis.id
}
