resource "google_secret_manager_secret" "oauth_credentials" {
  secret_id = "mcp-gcal-oauth-credentials"

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "oauth_credentials" {
  secret      = google_secret_manager_secret.oauth_credentials.id
  secret_data = file(var.oauth_credentials_file)
}
