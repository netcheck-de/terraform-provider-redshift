output "routines" {
  description = "Function and procedure lookups: overload signatures, owners, and import-compatible identities."
  value = {
    function = {
      id          = data.redshift_function.label.id
      signature   = data.redshift_function.label.signature
      return_type = data.redshift_function.label.return_type
      volatility  = data.redshift_function.label.volatility
      owner       = data.redshift_function.label.owner
    }
    procedure = {
      id        = data.redshift_procedure.scale.id
      signature = data.redshift_procedure.scale.signature
      security  = data.redshift_procedure.scale.security
      owner     = data.redshift_procedure.scale.owner
      outputs   = [for argument in data.redshift_procedure.scale.argument : argument.name if argument.mode == "OUT"]
    }
  }
}
