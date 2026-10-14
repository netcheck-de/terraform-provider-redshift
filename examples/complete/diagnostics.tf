# Diagnostics: configuration values of the provider's session, the lockout state of the example's password users,
# and an optional DEBUG grant for unredacted secure logging on a database with federated permissions.

variable "federated_debug_database" {
  description = "Database with federated permissions that the consumer workgroup can name, such as sales_db@finance-catalog; null skips the DEBUG grant, because the example does not register a warehouse with federated permissions."
  type        = string
  default     = null

  validation {
    condition     = var.federated_debug_database == null ? true : length(var.federated_debug_database) > 0
    error_message = "Supply a database name or leave federated_debug_database null."
  }
}

locals {
  debug_grant_enabled = var.federated_debug_database != null
}

data "redshift_configuration" "case_sensitive_identifiers" {
  provider = redshift.consumer
  name     = "enable_case_sensitive_identifier"
}

data "redshift_configuration" "search_path" {
  provider = redshift.consumer
  name     = "search_path"
}

# SHOW USER LOCKOUT needs a superuser, which the consumer provider's managed administrator secret is.
data "redshift_user_lockouts" "reader" {
  provider  = redshift.consumer
  user_name = redshift_user.reader.name
}

# DEBUG can be granted only to the administrators of the account that owns the producer, here the consumer account.
resource "redshift_debug_grant" "account_admins" {
  provider     = redshift.consumer
  count        = local.debug_grant_enabled ? 1 : 0
  database     = var.federated_debug_database
  grantee      = data.aws_caller_identity.consumer.account_id
  grantee_type = "ACCOUNT"
}

data "redshift_debug_grant" "account_admins" {
  provider     = redshift.consumer
  count        = local.debug_grant_enabled ? 1 : 0
  database     = redshift_debug_grant.account_admins[0].database
  grantee      = redshift_debug_grant.account_admins[0].grantee
  grantee_type = redshift_debug_grant.account_admins[0].grantee_type
}
