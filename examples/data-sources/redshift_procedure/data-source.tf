data "redshift_procedure" "purge" {
  database  = "analytics"
  schema    = "reporting"
  name      = "sp_purge_events"
  arguments = ["integer"]
}

output "purge_outputs" {
  value = [for argument in data.redshift_procedure.purge.argument : argument.name if argument.mode == "OUT"]
}
