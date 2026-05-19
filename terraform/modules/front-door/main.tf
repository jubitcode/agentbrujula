resource "azurerm_cdn_frontdoor_profile" "main" {
  name                = "fd-${var.name_prefix}"
  resource_group_name = var.resource_group_name
  sku_name            = "Standard_AzureFrontDoor" # ~$39/mo base, no WAF
  tags                = var.tags
}

resource "azurerm_cdn_frontdoor_endpoint" "main" {
  name                     = "agent-canonical"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.main.id
  enabled                  = true
  tags                     = var.tags
}

resource "azurerm_cdn_frontdoor_origin_group" "api" {
  name                     = "og-api"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.main.id
  session_affinity_enabled = false

  load_balancing {
    sample_size                        = 4
    successful_samples_required        = 3
    additional_latency_in_milliseconds = 50
  }

  health_probe {
    path                = "/health/stellar"
    protocol            = "Https"
    interval_in_seconds = 30
    request_type        = "GET"
  }
}

resource "azurerm_cdn_frontdoor_origin" "api" {
  name                          = "origin-api"
  cdn_frontdoor_origin_group_id = azurerm_cdn_frontdoor_origin_group.api.id

  enabled                        = true
  host_name                      = var.origin_hostname
  origin_host_header             = var.origin_host_header
  http_port                      = 80
  https_port                     = 443
  priority                       = 1
  weight                         = 1000
  certificate_name_check_enabled = true
}

resource "azurerm_cdn_frontdoor_route" "api" {
  name                          = "route-api"
  cdn_frontdoor_endpoint_id     = azurerm_cdn_frontdoor_endpoint.main.id
  cdn_frontdoor_origin_group_id = azurerm_cdn_frontdoor_origin_group.api.id
  cdn_frontdoor_origin_ids      = [azurerm_cdn_frontdoor_origin.api.id]

  enabled                = true
  forwarding_protocol    = "HttpsOnly"
  https_redirect_enabled = true
  patterns_to_match      = ["/*"]
  supported_protocols    = ["Http", "Https"]

  cdn_frontdoor_custom_domain_ids = var.custom_domain != "" ? [azurerm_cdn_frontdoor_custom_domain.main[0].id] : []

  link_to_default_domain = var.custom_domain == ""

  cache {
    query_string_caching_behavior = "UseQueryString"
    compression_enabled           = false
  }
}

# Custom domain — only created if var.custom_domain is set.
# Operator must add the validation TXT record (output below) at their
# DNS provider, then re-run `terraform apply` to let validation
# complete.
resource "azurerm_cdn_frontdoor_custom_domain" "main" {
  count = var.custom_domain != "" ? 1 : 0

  name                     = replace(var.custom_domain, ".", "-")
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.main.id
  host_name                = var.custom_domain

  tls {
    certificate_type = "ManagedCertificate"
    minimum_version  = "TLS12"
  }
}

# Diagnostic logs to Log Analytics
resource "azurerm_monitor_diagnostic_setting" "fd" {
  name                       = "fd-diagnostics"
  target_resource_id         = azurerm_cdn_frontdoor_profile.main.id
  log_analytics_workspace_id = var.log_analytics_id

  enabled_log {
    category = "FrontDoorAccessLog"
  }
  enabled_log {
    category = "FrontDoorHealthProbeLog"
  }

  enabled_metric {
    category = "AllMetrics"
  }
}
