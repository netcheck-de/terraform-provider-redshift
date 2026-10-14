data "redshift_copy_jobs" "analytics" {
  database = "analytics"
}

output "automatic_copy_jobs" {
  value = [for job in data.redshift_copy_jobs.analytics.copy_jobs : job.name if job.auto]
}
