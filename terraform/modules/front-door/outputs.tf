output "endpoint_host" {
  value = azurerm_cdn_frontdoor_endpoint.main.host_name
}

output "custom_domain_host" {
  value = var.custom_domain != "" ? var.custom_domain : ""
}

output "custom_domain_validation_token" {
  description = "TXT record value the operator must add at their DNS provider to validate the custom domain"
  value       = var.custom_domain != "" ? azurerm_cdn_frontdoor_custom_domain.main[0].validation_token : ""
  sensitive   = true
}

output "profile_resource_guid" {
  description = "Front Door profile resource GUID — sent in X-Azure-FDID header on every FD→origin request. ca-api middleware uses this to reject direct hits to the *.azurecontainerapps.io URL."
  value       = azurerm_cdn_frontdoor_profile.main.resource_guid
}
