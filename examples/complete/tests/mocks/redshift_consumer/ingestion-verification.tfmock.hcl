# These are catalog observations for concrete targets, not type-wide generated defaults.
override_data {
  target = data.redshift_template.csv_events
  values = {
    owner           = "observed_admin"
    format          = "CSV"
    format_argument = null
    parameters      = { IGNOREHEADER = "1", TIMEFORMAT = "auto", EMPTYASNULL = "", ENCODING = "UTF8" }
  }
}

override_data {
  target = data.redshift_template.json_events
  values = {
    owner           = "example_loader"
    format          = "JSON"
    format_argument = "auto ignorecase"
    parameters      = { MAXERROR = "10", TRUNCATECOLUMNS = "" }
  }
}

override_data {
  target = data.redshift_template.shapefile
  values = {
    format          = "SHAPEFILE"
    format_argument = "SIMPLIFY AUTO 0.000001"
    parameters      = {}
  }
}

override_data {
  target = data.redshift_object_grant.loader_template
  values = { privileges = ["USAGE"] }
}

override_data {
  target = data.redshift_copy_job.fixture
  values = {
    auto            = false
    iam_role        = "arn:aws:iam::111111111111:role/consumer-fixture"
    source          = "s3://mock-fixture/data/"
    format          = "CSV"
    format_argument = null
    parameters      = { IGNOREHEADER = "1" }
    table           = { schema = "example_schema", name = "example_copy_target", columns = ["id", "label"] }
  }
}

# Terraform 1.17 always generates empty computed nested lists for table columns,
# external_functions, and routine_parameters. Their blocking postconditions intentionally
# still reject those incomplete observations; no config-only fallback is used.
