data "azurerm_key_vault_secret" "kv_refs" {
  for_each = var.kv_secret_refs

  name         = each.value
  key_vault_id = var.key_vault_id
}

# Extract login server from the image string for the registry block
# (image format: "acr.azurecr.io/repo:tag")
locals {
  acr_login_server = split("/", var.image)[0]
}

resource "azurerm_container_app_job" "main" {
  name                         = var.name
  resource_group_name          = var.resource_group_name
  location                     = var.location
  container_app_environment_id = var.container_apps_environment_id
  tags                         = var.tags
  workload_profile_name        = "Consumption"

  replica_timeout_in_seconds = var.replica_timeout
  replica_retry_limit        = var.retry_limit

  schedule_trigger_config {
    cron_expression          = var.cron_expression
    parallelism              = 1
    replica_completion_count = 1
  }

  identity {
    type         = "UserAssigned"
    identity_ids = [var.identity_id]
  }

  registry {
    server   = local.acr_login_server
    identity = var.identity_id
  }

  dynamic "secret" {
    for_each = var.kv_secret_refs
    content {
      name                = lower(replace(secret.key, "_", "-"))
      identity            = var.identity_id
      key_vault_secret_id = data.azurerm_key_vault_secret.kv_refs[secret.key].versionless_id
    }
  }

  template {
    container {
      name    = var.name
      image   = var.image
      cpu     = var.cpu
      memory  = var.memory
      command = var.command

      dynamic "env" {
        for_each = var.env_plain
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = var.kv_secret_refs
        content {
          name        = env.key
          secret_name = lower(replace(env.key, "_", "-"))
        }
      }
    }
  }
}
