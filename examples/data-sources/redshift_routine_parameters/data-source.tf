data "redshift_routine_parameters" "upper" {
  database     = "warehouse"
  schema       = "public"
  routine_name = "f_upper"
  routine_type = "FUNCTION"
}

output "upper_inputs" {
  value = [for parameter in data.redshift_routine_parameters.upper.routine_parameters : parameter.data_type if parameter.mode == "IN"]
}
