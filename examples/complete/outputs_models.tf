output "model_checks" {
  description = "Models of the consumer-local schema, and the optional Bedrock model's inference function, owner, and reader privileges; the Bedrock entries are null unless bedrock_model_id is set."
  value = {
    schema_models = [for model in data.redshift_models.local.models : "${model.schema}.${model.name}"]
    summarizer = try({
      function          = "${data.redshift_external_model.summarizer[0].schema}.${data.redshift_external_model.summarizer[0].function}"
      model_id          = data.redshift_external_model.summarizer[0].model_id
      owner             = data.redshift_external_model.summarizer[0].owner
      reader_privileges = data.redshift_object_grant.readers_summarizer[0].privileges
    }, null)
  }
}
