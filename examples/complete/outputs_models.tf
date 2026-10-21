output "model_checks" {
  description = "Models of the consumer-local schema, and the optional Bedrock model's inference function, owner, and ROLE/USER/GROUP/PUBLIC privileges; the Bedrock entries are null unless bedrock_model_id is set."
  value = {
    schema_models = [for model in data.redshift_models.local.models : "${model.schema}.${model.name}"]
    summarizer = try({
      function                     = "${data.redshift_external_model.summarizer[0].schema}.${data.redshift_external_model.summarizer[0].function}"
      model_id                     = data.redshift_external_model.summarizer[0].model_id
      owner                        = data.redshift_external_model.summarizer[0].owner
      reader_privileges            = data.redshift_object_grant.readers_summarizer[0].privileges
      delegate_name                = data.redshift_user.model_delegate[0].name
      delegate_schema_privileges   = data.redshift_object_grant.model_delegate_schema[0].privileges
      user_privileges              = data.redshift_object_grant.delegate_summarizer[0].privileges
      user_grant_option_privileges = data.redshift_object_grant.delegate_summarizer[0].grant_option_privileges
      group_privileges             = data.redshift_object_grant.group_summarizer[0].privileges
      public_privileges            = data.redshift_object_grant.public_summarizer[0].privileges
    }, null)
  }
}
