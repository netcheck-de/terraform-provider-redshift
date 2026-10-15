data "redshift_models" "ml" {
  database = "analytics"
  schema   = "ml"
}

output "ml_model_names" {
  value = data.redshift_models.ml.models[*].name
}
