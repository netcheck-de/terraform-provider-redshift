data "redshift_role_grant" "reader" {
  role    = "report_readers"
  to_user = "report_reader"
}
