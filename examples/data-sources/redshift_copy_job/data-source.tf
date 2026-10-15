data "redshift_copy_job" "events" {
  database = "analytics"
  name     = "events_auto_load"
}

output "events_job_auto" {
  value = data.redshift_copy_job.events.auto
}
