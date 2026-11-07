output "datasharing" {
  description = "Producer share metadata, membership and consumer listings, datashare listings on both sides, and datashare permissions."
  value = {
    producer_owner            = data.redshift_datashare.producer.owner
    producer_share_id         = data.redshift_datashare.producer.share_id
    producer_namespace        = data.redshift_datashare.producer.producer_namespace
    producer_schemas          = { for item in data.redshift_datashare_schemas.producer.datashare_schemas : item.schema => item.include_new }
    producer_tables           = [for item in data.redshift_datashare_tables.producer.datashare_tables : "${item.schema}.${item.table}"]
    membership_coverage       = { for item in data.redshift_datashare_memberships.producer.datashare_memberships : "${item.schema}/${item.kind}" => item.all }
    consumers                 = [for grant in data.redshift_datashare_grants.producer.datashare_grants : coalesce(grant.namespace_id, grant.account_id)]
    bulk_membership           = { for kind, membership in data.redshift_datashare_membership.bulk : kind => membership.all }
    outbound_shares           = [for share in data.redshift_datashares.outbound.datashares : share.name]
    inbound_shares            = [for share in data.redshift_inbound_datashares.consumer.inbound_datashares : { name = share.name, consumer_database = share.consumer_database }]
    share_operator_privileges = data.redshift_datashare_privilege.share_operators.privileges
    grants_share_privileges   = { for item in data.redshift_datashare_privileges.grants.datashare_privileges : "${item.grantee_type}:${item.grantee}" => item.privileges }
  }
}
