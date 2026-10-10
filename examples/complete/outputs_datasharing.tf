output "datasharing" {
  description = "Producer share metadata, datashare listings on both sides, and the share operators' datashare permissions."
  value = {
    producer_owner            = data.redshift_datashare.producer.owner
    producer_share_id         = data.redshift_datashare.producer.share_id
    producer_namespace        = data.redshift_datashare.producer.producer_namespace
    outbound_shares           = [for share in data.redshift_datashares.outbound.items : share.name]
    inbound_shares            = [for share in data.redshift_datashares.inbound.items : { name = share.name, consumer_database = share.consumer_database }]
    share_operator_privileges = data.redshift_datashare_privilege.share_operators.privileges
  }
}
