data "redshift_group_membership" "reader" {
  group = "report_readers"
  user  = "report_reader"
}
