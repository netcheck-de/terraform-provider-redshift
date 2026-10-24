import {
  to = redshift_external_model.summarizer
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "ml"
    name           = "summarizer"
  })
}
