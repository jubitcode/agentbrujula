output "resource_group_name" {
  value = azurerm_resource_group.main.name
}

output "location" {
  value = azurerm_resource_group.main.location
}

output "tags" {
  value = local.tags
}

output "container_apps_environment_id" {
  value = module.container_apps_env.id
}

output "container_apps_environment_default_domain" {
  value = module.container_apps_env.default_domain
}

output "log_analytics_workspace_id" {
  value = module.container_apps_env.log_analytics_workspace_id
}

output "managed_identity_id" {
  value = module.identity.id
}

output "managed_identity_principal_id" {
  value = module.identity.principal_id
}

output "acr_login_server" {
  value = module.acr.login_server
}

output "acr_id" {
  value = module.acr.id
}

output "acr_name" {
  value = module.acr.name
}

output "key_vault_id" {
  value = module.keyvault.id
}

output "key_vault_uri" {
  value = module.keyvault.vault_uri
}

output "key_vault_name" {
  value = module.keyvault.name
}

output "postgres_fqdn" {
  value = module.postgres.fqdn
}

output "postgres_admin_username" {
  value = module.postgres.admin_username
}

output "postgres_database_name" {
  value = module.postgres.database_name
}

output "postgres_connection_secret_name" {
  value = module.postgres.connection_string_secret_name
}

output "postgres_password_secret_name" {
  value = module.postgres.password_secret_name
}

output "redis_password_secret_name" {
  value = module.redis.password_secret_name
}

output "redis_host" {
  value = module.redis.host
}

output "session_token_secret_name" {
  value = azurerm_key_vault_secret.session_token_secret.name
}
