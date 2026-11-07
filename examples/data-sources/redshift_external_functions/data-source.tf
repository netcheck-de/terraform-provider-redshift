data "redshift_external_functions" "serving" {
  database = "warehouse"
  schema   = "serving"
}

output "lambda_udfs" {
  value = [for function in data.redshift_external_functions.serving.external_functions : "${function.name}(${join(", ", function.arguments)}) -> ${function.return_type}"]
}
