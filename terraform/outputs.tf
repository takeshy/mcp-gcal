output "load_balancer_ip" {
  description = "Global static IP address for DNS A record"
  value       = google_compute_global_address.default.address
}

output "cloud_run_url" {
  description = "Cloud Run service URL"
  value       = google_cloud_run_v2_service.app.uri
}

output "service_url" {
  description = "Public service URL"
  value       = "https://${var.domain}"
}
