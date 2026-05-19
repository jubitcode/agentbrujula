variable "subscription_id" {
  type        = string
  description = "Azure subscription ID (UUID). Set in prod.tfvars."
}

variable "tenant_id" {
  type        = string
  description = "Azure AD tenant ID (UUID)."
}

variable "location" {
  type        = string
  description = "Primary Azure region. Default mexicocentral; fall back to eastus2 if any SKU is unavailable."
  default     = "mexicocentral"
}

variable "environment" {
  type        = string
  description = "Environment name — prod / staging. Used in resource names + tags."
  default     = "prod"
}

variable "operator_object_id" {
  type        = string
  description = "AAD object ID of the operator running terraform apply. Gets Key Vault Secrets Officer + Storage Blob Data Contributor for state."
}

variable "custom_domain" {
  type        = string
  description = "Custom domain for Front Door (e.g. api.agentcanonical.com). Leave empty to use the FD default *.azurefd.net hostname."
  default     = ""
}

variable "vercel_origin_patterns" {
  type        = list(string)
  description = "Vercel deploy URLs allowed via CORS on the api. Use the project's production domain + preview pattern."
  default = [
    "https://agentcanonical.com",
    "https://www.agentcanonical.com",
    # Preview URLs follow https://<project>-<hash>-<scope>.vercel.app —
    # CORS needs full origin strings; preview hashes change per PR, so
    # operators may need to set this to a single wildcard at the api layer.
  ]
}

variable "stellar_network" {
  type        = string
  description = "testnet | public"
  default     = "testnet"
  validation {
    condition     = contains(["testnet", "public"], var.stellar_network)
    error_message = "stellar_network must be 'testnet' or 'public'."
  }
}

variable "stellar_rpc_url" {
  type        = string
  description = "Soroban RPC URL"
  default     = "https://soroban-testnet.stellar.org"
}

variable "stellar_horizon_url" {
  type        = string
  description = "Stellar Horizon URL"
  default     = "https://horizon-testnet.stellar.org"
}

variable "x402_usdc_address" {
  type        = string
  description = "USDC SAC contract ID"
  default     = "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ"
}

variable "x402_escrow_contract" {
  type        = string
  description = "usdc-escrow contract ID (testnet default; bump on mainnet deploy)"
  default     = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO"
}

variable "tool_registry_contract_id" {
  type        = string
  description = "tool-registry contract ID (testnet default; bump on mainnet deploy)"
  default     = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW"
}

variable "api_image_tag" {
  type        = string
  description = "ACR image tag for ca-api. Use git SHA — 'latest' is rejected for prod."

  validation {
    condition     = var.environment != "prod" || var.api_image_tag != "latest"
    error_message = "api_image_tag must be an immutable tag (git SHA) when environment=prod. 'latest' is mutable and breaks rollback."
  }
}

variable "mcp_meta_image_tag" {
  type        = string
  description = "ACR image tag for ca-mcp-meta. Use git SHA — 'latest' is rejected for prod."

  validation {
    condition     = var.environment != "prod" || var.mcp_meta_image_tag != "latest"
    error_message = "mcp_meta_image_tag must be an immutable tag (git SHA) when environment=prod."
  }
}

variable "keyvault_allowed_ip_ranges" {
  type        = list(string)
  description = "Operator IP CIDRs allowed to hit Key Vault (e.g. ['198.51.100.5/32']). Empty list = vault publicly reachable for seed phase. Once secrets are seeded, set this to your IP and re-apply — vault flips to default_action=Deny."
  default     = []
}

variable "tags" {
  type        = map(string)
  description = "Common tags applied to every resource"
  default = {
    project   = "agent-canonical"
    managedBy = "terraform"
  }
}
