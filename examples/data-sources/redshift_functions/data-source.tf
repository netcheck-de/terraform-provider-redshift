data "redshift_functions" "public" {
  database = "warehouse"
  schema   = "public"
}

output "lambda_udfs" {
  value = [for function in data.redshift_functions.public.items : function.name if function.language == "exfunc"]
}
