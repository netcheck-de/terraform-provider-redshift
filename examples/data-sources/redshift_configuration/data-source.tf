data "redshift_configuration" "case_sensitive" {
  name = "enable_case_sensitive_identifier"
}

data "redshift_configuration" "search_path" {
  name = "search_path"
}

output "provider_session_settings" {
  value = {
    case_sensitive_identifiers = data.redshift_configuration.case_sensitive.value
    search_path                = data.redshift_configuration.search_path.value
  }
}
