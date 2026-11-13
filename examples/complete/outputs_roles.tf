output "role_details" {
  description = "Role ownership and IDs, and the loader's admin option on the operators role, as observed by the lookups."
  value = {
    readers = {
      id          = data.redshift_role.readers.role_id
      owner       = data.redshift_role.readers.owner
      external_id = data.redshift_role.readers.external_id
    }
    auditors = {
      id    = data.redshift_role.auditors.role_id
      owner = data.redshift_role.auditors.owner
    }
    loader_operators = {
      exists       = data.redshift_role_grant.loader_operators.exists
      admin_option = data.redshift_role_grant.loader_operators.admin_option
    }
  }
}

output "identity_provider_details" {
  description = "Catalog details of the optional Identity Center provider; null when SSO is disabled."
  value = try({
    type                         = data.redshift_identity_provider.this[0].type
    provider_id                  = data.redshift_identity_provider.this[0].provider_id
    instance_id                  = data.redshift_identity_provider.this[0].instance_id
    identity_center_instance_arn = data.redshift_identity_provider.this[0].identity_center_instance_arn
    enabled                      = data.redshift_identity_provider.this[0].enabled
  }, null)
}

output "role_listings" {
  description = "Warehouse-wide role listing, the recipients of the operators role, and the optional identity providers."
  value = {
    roles = [for role in data.redshift_roles.all.roles : role.name]
    operator_recipients = [
      for grant in data.redshift_role_grants.operators.role_grants : coalesce(grant.to_user, grant.to_role)
    ]
    identity_providers = try([for provider in data.redshift_identity_providers.all[0].identity_providers : provider.name], null)
  }
}
