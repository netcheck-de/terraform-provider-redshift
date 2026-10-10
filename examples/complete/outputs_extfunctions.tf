output "extfunction_checks" {
  description = "Observed Lambda UDF signature, consumer-schema procedures, and the UDF's parameters."
  value = {
    lambda_udfs = [
      for function in data.redshift_functions.example.items :
      "${function.schema}.${function.name}(${join(", ", function.arguments)}) -> ${function.return_type}"
      if function.language == "exfunc"
    ]
    procedures       = [for procedure in data.redshift_procedures.example.items : "${procedure.schema}.${procedure.name}"]
    upper_parameters = [for parameter in data.redshift_routine_parameters.upper.items : "${parameter.mode} ${parameter.data_type}"]
  }
}
