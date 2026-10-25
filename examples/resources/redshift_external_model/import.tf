import {
  to = redshift_external_model.summarizer
  id = jsonencode({
    database = "analytics"
    schema   = "ml"
    name     = "summarizer"
  })
}
