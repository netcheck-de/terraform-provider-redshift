# Diagnostics: configuration values of the provider's session, the optional lockout state of the example's password
# users, and an optional DEBUG grant for unredacted secure logging on a database with federated permissions.

variable "user_lockouts" {
  description = "Read the reader's lockout state with SHOW USER LOCKOUT. Off by default, because warehouses without account lockout support reject the command with \"account lockout security is not supported\"."
  type        = bool
  default     = false
}

variable "session_context_variable" {
  description = "Optional dotted context variable to read in the consumer provider's own session, such as app_context.user_id. Leave null unless that variable exists in newly opened administrator sessions; a SET in a separate session does not persist."
  type        = string
  default     = null

  validation {
    condition     = var.session_context_variable == null ? true : can(regex("^[a-z_][a-z0-9_]{0,126}(\\.[a-z_][a-z0-9_]{0,126})+$", var.session_context_variable))
    error_message = "Supply a lowercase dotted context variable name or leave session_context_variable null."
  }
}

variable "federated_debug_database" {
  description = "Producer-local database with federated permissions hosted by the warehouse bound to redshift.consumer, such as sales_db. This ACCOUNT example cannot use db@catalog because remote ACCOUNT readback is unavailable. Null skips the grant; the example does not register federated permissions."
  type        = string
  default     = null

  validation {
    condition     = var.federated_debug_database == null ? true : length(var.federated_debug_database) > 0 && !strcontains(var.federated_debug_database, "@")
    error_message = "Supply a nonempty producer-local name without @catalog, or leave federated_debug_database null."
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

data "redshift_configuration" "session_context" {
  provider   = redshift.consumer
  count      = var.session_context_variable == null ? 0 : 1
  name       = var.session_context_variable
  depends_on = [aws_redshiftserverless_workgroup.consumer]
}

# SHOW USER LOCKOUT needs a superuser, which the consumer provider's managed administrator secret is.
data "redshift_user_lockouts" "reader" {
  provider  = redshift.consumer
  count     = var.user_lockouts ? 1 : 0
  user_name = redshift_user.reader.name
}

# ACCOUNT readback requires the warehouse bound to this provider to host the federated database itself. The alias
# named consumer is semantically the federation producer here; remote db@catalog ACCOUNT grants are unsupported.
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
