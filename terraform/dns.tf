locals {
  dns_project = var.dns_project_id != "" ? var.dns_project_id : var.project_id
}

# Reference the existing DNS zone (may be in a different project)
data "google_dns_managed_zone" "default" {
  name    = var.dns_zone_name
  project = local.dns_project
}

resource "google_dns_record_set" "mcp_a" {
  project      = local.dns_project
  name         = "${var.domain}."
  type         = "A"
  ttl          = 300
  managed_zone = data.google_dns_managed_zone.default.name
  rrdatas      = [google_compute_global_address.default.address]
}
