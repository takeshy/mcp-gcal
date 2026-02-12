# Static global IP
resource "google_compute_global_address" "default" {
  name = "mcp-gcal-ip"

  depends_on = [google_project_service.apis]
}

# Serverless NEG → Cloud Run
resource "google_compute_region_network_endpoint_group" "cloud_run" {
  name                  = "mcp-gcal-neg"
  region                = var.region
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.app.name
  }

  depends_on = [google_project_service.apis]
}

# Backend service
resource "google_compute_backend_service" "default" {
  name                  = "mcp-gcal-backend"
  protocol              = "HTTP"
  load_balancing_scheme = "EXTERNAL_MANAGED"

  backend {
    group = google_compute_region_network_endpoint_group.cloud_run.id
  }
}

# HTTPS URL map
resource "google_compute_url_map" "https" {
  name            = "mcp-gcal-https"
  default_service = google_compute_backend_service.default.id
}

# Google-managed SSL certificate
resource "google_compute_managed_ssl_certificate" "default" {
  name = "mcp-gcal-cert"

  managed {
    domains = [var.domain]
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [google_project_service.apis]
}

# HTTPS proxy
resource "google_compute_target_https_proxy" "default" {
  name             = "mcp-gcal-https-proxy"
  url_map          = google_compute_url_map.https.id
  ssl_certificates = [google_compute_managed_ssl_certificate.default.id]
}

# HTTPS forwarding rule (port 443)
resource "google_compute_global_forwarding_rule" "https" {
  name                  = "mcp-gcal-https-rule"
  target                = google_compute_target_https_proxy.default.id
  port_range            = "443"
  ip_address            = google_compute_global_address.default.id
  load_balancing_scheme = "EXTERNAL_MANAGED"
}

# --- HTTP → HTTPS redirect ---

resource "google_compute_url_map" "http_redirect" {
  name = "mcp-gcal-http-redirect"

  default_url_redirect {
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = false
  }

  depends_on = [google_project_service.apis]
}

resource "google_compute_target_http_proxy" "redirect" {
  name    = "mcp-gcal-http-proxy"
  url_map = google_compute_url_map.http_redirect.id
}

resource "google_compute_global_forwarding_rule" "http" {
  name                  = "mcp-gcal-http-rule"
  target                = google_compute_target_http_proxy.redirect.id
  port_range            = "80"
  ip_address            = google_compute_global_address.default.id
  load_balancing_scheme = "EXTERNAL_MANAGED"
}
