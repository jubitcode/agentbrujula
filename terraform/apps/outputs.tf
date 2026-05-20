output "api_fqdn" {
  value       = module.container_app_api.fqdn
  description = "Public ingress FQDN for ca-api. Use this for phase-6 smoke tests before Front Door is in front of it."
}

output "mcp_meta_fqdn" {
  value       = module.container_app_mcp_meta.fqdn
  description = "Internal-only ingress FQDN for ca-mcp-meta. Only reachable from inside the Container Apps Environment."
}

output "front_door_endpoint_host" {
  value       = try(module.front_door[0].endpoint_host, null)
  description = "Front Door default endpoint hostname. Null until deploy_front_door=true."
}

output "front_door_custom_domain_validation_token" {
  value       = try(module.front_door[0].custom_domain_validation_token, null)
  sensitive   = true
  description = "TXT record value to add at your DNS provider to validate the custom domain. Null when deploy_front_door=false or custom_domain is empty."
}
