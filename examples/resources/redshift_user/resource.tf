resource "redshift_user" "grafana" {
  name                = "grafana"
  password_wo         = random_password.grafana.result
  password_wo_version = 0

  connection_limit = 10
  session_timeout  = 3600
  valid_until      = "2030-01-01T00:00:00Z"
  search_path      = ["reporting", "public"]
  session_defaults = {
    timezone          = "Europe/Berlin"
    statement_timeout = "300000"
  }
}

# Signs in only with temporary IAM credentials; no password exists.
resource "redshift_user" "etl" {
  name              = "etl"
  password_disabled = true
  syslog_access     = "UNRESTRICTED"
}

resource "redshift_role_grant" "grafana_monitor" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}
