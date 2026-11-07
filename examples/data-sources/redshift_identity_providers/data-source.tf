data "redshift_identity_providers" "all" {}

output "enabled_identity_namespaces" {
  value = [for provider in data.redshift_identity_providers.all.identity_providers : provider.namespace if provider.enabled]
}
