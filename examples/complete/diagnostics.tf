# Diagnostics: configuration values of the provider's session, the optional lockout state of the example's password
# users, and an optional DEBUG grant for unredacted secure logging on a database with federated permissions.

variable "user_lockouts" {
  description = "Read the reader's lockout state with SHOW USER LOCKOUT. Off by default, because warehouses without account lockout support reject the command with \"account lockout security is not supported\"."
  type        = bool
  default     = false
}

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

# Lookups with only constant inputs are read while planning; the dependency defers them until the workgroup and its
# parameters exist.
data "redshift_configuration" "case_sensitive_identifiers" {
  provider   = redshift.consumer
  name       = "enable_case_sensitive_identifier"
  depends_on = [aws_redshiftserverless_workgroup.consumer]
}

data "redshift_configuration" "search_path" {
  provider   = redshift.consumer
  name       = "search_path"
  depends_on = [aws_redshiftserverless_workgroup.consumer]
}

# SHOW USER LOCKOUT needs a superuser, which the consumer provider's managed administrator secret is.
data "redshift_user_lockouts" "reader" {
  provider  = redshift.consumer
  count     = var.user_lockouts ? 1 : 0
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
