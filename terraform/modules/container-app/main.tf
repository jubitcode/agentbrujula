locals {
  # All KV-backed env vars in one map for the secret block
  all_kv_refs = merge(var.kv_secret_refs, var.kv_secret_refs_for_built_strings)
}

# Look up the secret URIs (without versions) so the secret block can
# reference them. Container Apps follows the secret without a version
# pin so revision restarts pick up rotated values.
data "azurerm_key_vault_secret" "kv_refs" {
  for_each = local.all_kv_refs

  name         = each.value
  key_vault_id = var.key_vault_id
}

resource "azurerm_container_app" "main" {
  name                         = var.name
  resource_group_name          = var.resource_group_name
  container_app_environment_id = var.container_apps_environment_id
  revision_mode                = "Single"
  tags                         = var.tags
  workload_profile_name        = "Consumption"

  identity {
    type         = "UserAssigned"
    identity_ids = [var.identity_id]
  }

  registry {
    server   = var.acr_login_server
    identity = var.identity_id
  }

  # One secret block per Key Vault reference. The container app
  # streams the secret value via the user-assigned MI; rotation
  # happens by revision restart, not live.
  dynamic "secret" {
    for_each = local.all_kv_refs
    content {
      name                = lower(replace(secret.key, "_", "-"))
      identity            = var.identity_id
      key_vault_secret_id = data.azurerm_key_vault_secret.kv_refs[secret.key].versionless_id
    }
  }

  ingress {
    external_enabled = var.ingress_external
    target_port      = var.target_port
    transport        = "http"

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }

  template {
    min_replicas = var.min_replicas
    max_replicas = var.max_replicas

    container {
      name    = var.name
      image   = var.image
      cpu     = var.cpu
      memory  = var.memory
      command = var.command

      # Plain env vars
      dynamic "env" {
        for_each = var.env_plain
        content {
          name  = env.key
          value = env.value
        }
      }

      # KV-backed secret env vars
      dynamic "env" {
        for_each = local.all_kv_refs
        content {
          name        = env.key
          secret_name = lower(replace(env.key, "_", "-"))
        }
      }

      dynamic "liveness_probe" {
        for_each = var.liveness_probe_path != "" ? [1] : []
        content {
          path                    = var.liveness_probe_path
          port                    = var.target_port
          transport               = "HTTP"
          initial_delay           = 30
          interval_seconds        = 30
          timeout                 = 5
          failure_count_threshold = 3
        }
      }

      dynamic "readiness_probe" {
        for_each = var.readiness_probe_path != "" ? [1] : []
        content {
          path                    = var.readiness_probe_path
          port                    = var.target_port
          transport               = "HTTP"
          interval_seconds        = 10
          timeout                 = 5
          failure_count_threshold = 3
          success_count_threshold = 1
        }
      }
    }

    # HTTP concurrency-based autoscale
    http_scale_rule {
      name                = "http-concurrency"
      concurrent_requests = var.http_concurrency
    }
  }
}
