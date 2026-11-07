data "redshift_functions" "public" {
  database = "warehouse"
  schema   = "public"
}

output "immutable_udfs" {
  value = [for function in data.redshift_functions.public.functions : "${function.name}(${function.signature})" if function.volatility == "IMMUTABLE"]
}
