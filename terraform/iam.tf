resource "google_service_account" "cloud_run" {
  account_id   = "mcp-gcal-run"
  display_name = "MCP GCal Cloud Run"

  depends_on = [google_project_service.apis]
}

# Grant Cloud Run SA access to read the OAuth credentials secret
resource "google_secret_manager_secret_iam_member" "cloud_run_oauth_credentials" {
  secret_id = google_secret_manager_secret.oauth_credentials.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

# Cloud Build SA permissions
locals {
  cloud_build_sa = "serviceAccount:${var.project_number}@cloudbuild.gserviceaccount.com"
}

resource "google_project_iam_member" "cloudbuild_run_admin" {
  project = var.project_id
  role    = "roles/run.admin"
  member  = local.cloud_build_sa

  depends_on = [google_project_service.apis]
}

resource "google_project_iam_member" "cloudbuild_sa_user" {
  project = var.project_id
  role    = "roles/iam.serviceAccountUser"
  member  = local.cloud_build_sa

  depends_on = [google_project_service.apis]
}
