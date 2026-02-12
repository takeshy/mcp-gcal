resource "google_cloud_run_v2_service" "app" {
  name                = "mcp-gcal"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
  deletion_protection = false

  template {
    service_account = google_service_account.cloud_run.email

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    volumes {
      name = "oauth-credentials"
      secret {
        secret       = google_secret_manager_secret.oauth_credentials.secret_id
        default_mode = 292 # 0444
        items {
          version = "latest"
          path    = "credentials.json"
        }
      }
    }

    containers {
      image = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.app.repository_id}/mcp-gcal:latest"

      args = [
        "--mode=http",
        "--addr=:8080",
        "--base-url=https://${var.domain}",
        "--credentials-file=/secrets/credentials.json",
        "--db=/data/mcp-gcal.db",
      ]

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          memory = "256Mi"
          cpu    = "1"
        }
        cpu_idle = true
      }

      volume_mounts {
        name       = "oauth-credentials"
        mount_path = "/secrets"
      }

      startup_probe {
        http_get {
          path = "/health"
        }
        initial_delay_seconds = 3
        period_seconds        = 5
        failure_threshold     = 3
      }
    }
  }

  depends_on = [
    google_project_service.apis,
    google_secret_manager_secret_version.oauth_credentials,
    google_secret_manager_secret_iam_member.cloud_run_oauth_credentials,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "public" {
  name     = google_cloud_run_v2_service.app.name
  location = google_cloud_run_v2_service.app.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}
