variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "project_number" {
  description = "GCP project number"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run and Artifact Registry"
  type        = string
  default     = "asia-northeast1"
}

variable "domain" {
  description = "Subdomain for the MCP server"
  type        = string
  default     = "mcp.gemihub.online"
}

variable "dns_zone_name" {
  description = "Existing Cloud DNS managed zone name"
  type        = string
  default     = "gemihub-online"
}

variable "dns_project_id" {
  description = "GCP project ID where the DNS zone is hosted (if different from project_id)"
  type        = string
  default     = ""
}

variable "oauth_credentials_file" {
  description = "Path to Google OAuth2 credentials JSON file (web application type)"
  type        = string
  default     = "credentials.json"
}
