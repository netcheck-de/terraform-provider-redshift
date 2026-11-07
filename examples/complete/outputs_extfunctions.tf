output "extfunction_checks" {
  description = "Observed Lambda UDF signatures and owners, and the UDF's parameters."
  value = {
    lambda_udfs = [
      for function in data.redshift_external_functions.example.external_functions :
      "${function.schema}.${function.name}(${join(", ", function.arguments)}) -> ${function.return_type}"
    ]
    lambda_owners    = { for function in data.redshift_external_functions.example.external_functions : function.name => function.owner... }
    upper_parameters = [for parameter in data.redshift_routine_parameters.upper.routine_parameters : "${parameter.mode} ${parameter.data_type}"]
  }
}
