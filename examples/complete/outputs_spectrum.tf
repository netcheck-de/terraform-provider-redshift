output "spectrum_table" {
  description = "Observed definition of the SQL-managed Spectrum table and the location of its first partition."
  value = {
    columns         = [for column in data.redshift_external_table.events.column : "${column.name} ${column.type}"]
    partition_keys  = [for key in data.redshift_external_table.events.partition_key : key.name]
    location        = data.redshift_external_table.events.location
    input_format    = data.redshift_external_table.events.input_format
    first_partition = data.redshift_external_partition.events_first_day.location
  }
}

output "spectrum_parquet_table" {
  description = "Observed Parquet definition; lookup columns follow catalog order, not the resource's configured order."
  value = {
    columns      = [for column in data.redshift_external_table.parquet_events.column : "${column.name} ${column.type}"]
    location     = data.redshift_external_table.parquet_events.location
    input_format = data.redshift_external_table.parquet_events.input_format
  }
}

output "spectrum_listings" {
  description = "External tables of the Glue-backed schema with their locations, and the managed table's partitions."
  value = {
    tables     = { for table in data.redshift_external_tables.glue.external_tables : table.name => table.location }
    partitions = [for partition in data.redshift_external_partitions.events.external_partitions : partition.location]
  }
}
