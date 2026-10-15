output "diagnostics" {
  description = "Session configuration of the consumer provider, and the optional reader lockout state and DEBUG grant."
  value = {
    case_sensitive_identifiers = data.redshift_configuration.case_sensitive_identifiers.value
    search_path                = data.redshift_configuration.search_path.value
    reader_lockouts = var.user_lockouts ? [
      for lockout in data.redshift_user_lockouts.reader[0].user_lockouts : {
        locked                = lockout.is_locked
        reason                = lockout.lock_reason
        failed_login_attempts = lockout.failed_login_attempts
      }
    ] : null
    account_admins_debug = try(data.redshift_debug_grant.account_admins[0].exists, null)
  }
}
