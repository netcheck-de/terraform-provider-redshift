data "redshift_procedures" "etl" {
  database = "warehouse"
  schema   = "etl"
}

output "definer_procedures" {
  value = [for procedure in data.redshift_procedures.etl.procedures : procedure.name if procedure.security == "DEFINER"]
}
