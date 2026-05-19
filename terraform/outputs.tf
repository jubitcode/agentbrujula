output "api_default_fqdn" {
  description = "ca-api default Azure FQDN (use for direct testing; production traffic goes via Front Door)"
  value       = module.container_app_api.fqdn
}

output "mcp_meta_internal_fqdn" {
  description = "ca-mcp-meta internal FQDN (only reachable from within the CA environment)"
  value       = module.container_app_mcp_meta.fqdn
}

output "front_door_endpoint" {
  description = "Front Door default endpoint hostname"
  value       = module.front_door.endpoint_host
}

output "front_door_custom_domain" {
  description = "Front Door custom domain (if configured)"
  value       = module.front_door.custom_domain_host
}

output "acr_login_server" {
  description = "ACR registry hostname for docker push"
  value       = module.acr.login_server
}

output "key_vault_uri" {
  description = "Key Vault URI for `az keyvault secret set` commands"
  value       = module.keyvault.vault_uri
}

output "postgres_fqdn" {
  description = "PostgreSQL Flexible Server FQDN (private — only reachable from snet-aca)"
  value       = module.postgres.fqdn
  sensitive   = false
}

output "redis_host" {
  description = "Azure Cache for Redis hostname (private)"
  value       = module.redis.host
}
