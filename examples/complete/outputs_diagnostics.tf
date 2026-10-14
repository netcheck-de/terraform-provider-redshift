output "diagnostics" {
  description = "Session configuration of the consumer provider, the reader's lockout state, and the optional DEBUG grant."
  value = {
    case_sensitive_identifiers = data.redshift_configuration.case_sensitive_identifiers.value
    search_path                = data.redshift_configuration.search_path.value
    reader_lockouts = [
      for lockout in data.redshift_user_lockouts.reader.user_lockouts : {
        locked                = lockout.is_locked
        reason                = lockout.lock_reason
        failed_login_attempts = lockout.failed_login_attempts
      }
    ]
    account_admins_debug = try(data.redshift_debug_grant.account_admins[0].exists, null)
  }
}
