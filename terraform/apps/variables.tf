variable "subscription_id" {
  type        = string
  description = "Azure subscription ID (UUID). Must match the platform layer."
}

variable "tenant_id" {
  type        = string
  description = "Azure AD tenant ID (UUID)."
}

variable "location" {
  type        = string
  description = "Primary Azure region. Must match the platform layer."
  default     = "eastus2"
}

variable "environment" {
  type        = string
  description = "Environment name (prod / staging). Drives image-tag validation."
  default     = "prod"
}

variable "api_image_tag" {
  type        = string
  description = "ACR image tag for ca-api. Use a git SHA; 'latest' is rejected for prod."

  validation {
    condition     = var.environment != "prod" || var.api_image_tag != "latest"
    error_message = "api_image_tag must be an immutable tag (git SHA) when environment=prod."
  }
}

variable "mcp_meta_image_tag" {
  type        = string
  description = "ACR image tag for ca-mcp-meta. Use a git SHA; 'latest' is rejected for prod."

  validation {
    condition     = var.environment != "prod" || var.mcp_meta_image_tag != "latest"
    error_message = "mcp_meta_image_tag must be an immutable tag (git SHA) when environment=prod."
  }
}

variable "stellar_network" {
  type    = string
  default = "testnet"

  validation {
    condition     = contains(["testnet", "public"], var.stellar_network)
    error_message = "stellar_network must be 'testnet' or 'public'."
  }
}

variable "stellar_rpc_url" {
  type    = string
  default = "https://soroban-testnet.stellar.org"
}

variable "stellar_horizon_url" {
  type    = string
  default = "https://horizon-testnet.stellar.org"
}

variable "x402_usdc_address" {
  type    = string
  default = "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ"
}

variable "x402_escrow_contract" {
  type    = string
  default = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO"
}

variable "tool_registry_contract_id" {
  type    = string
  default = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW"
}

variable "vercel_origin_patterns" {
  type        = list(string)
  description = "Vercel deploy URLs allowed via CORS on ca-api."
  default     = []
}

variable "deploy_front_door" {
  type        = bool
  description = "Bring up Azure Front Door + custom domain in this apply. Default false; flip to true after the apps are smoke-tested on the *.azurecontainerapps.io ingress."
  default     = false
}

variable "custom_domain" {
  type        = string
  description = "Custom domain for Front Door (e.g. api.agentcanonical.com). Only used when deploy_front_door=true. Empty = use the default *.azurefd.net hostname."
  default     = ""
}

variable "tags" {
  type = map(string)
  default = {
    project   = "agent-canonical"
    managedBy = "terraform"
  }
}
