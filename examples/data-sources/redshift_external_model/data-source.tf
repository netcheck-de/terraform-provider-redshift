data "redshift_external_model" "summarizer" {
  database = "analytics"
  schema   = "ml"
  name     = "summarizer"
}

output "summarizer_function" {
  value = "${data.redshift_external_model.summarizer.schema}.${data.redshift_external_model.summarizer.function}"
}
