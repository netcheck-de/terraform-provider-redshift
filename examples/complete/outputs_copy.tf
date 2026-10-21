output "copy_checks" {
  description = "Read-only observations of the COPY templates and the optional copy job."
  value = {
    template_format     = data.redshift_template.csv_events.format
    template_owner      = data.redshift_template.csv_events.owner
    template_parameters = data.redshift_template.csv_events.parameters
    schema_templates    = [for template in data.redshift_templates.local.templates : template.name]
    shapefile_format    = data.redshift_template.shapefile.format
    shapefile_simplify  = data.redshift_template.shapefile.format_argument
    database_templates  = [for template in data.redshift_templates.database.templates : "${template.schema}.${template.name}"]
    copy_job_auto       = one(data.redshift_copy_job.fixture[*].auto)
    copy_job_role       = one(data.redshift_copy_job.fixture[*].iam_role)
    copy_job_source     = one(data.redshift_copy_job.fixture[*].source)
    copy_job_table      = one(data.redshift_copy_job.fixture[*].table)
    copy_job_format     = one(data.redshift_copy_job.fixture[*].format)
    copy_job_parameters = one(data.redshift_copy_job.fixture[*].parameters)
    copy_jobs           = [for job in data.redshift_copy_jobs.local.copy_jobs : job.name]
  }
}
