# An IAM user sees the unredacted secure logging records of its own queries on the database.
resource "redshift_debug_grant" "auditor" {
  database     = "sales_db@finance-catalog"
  grantee      = "IAM:sales_auditor"
  grantee_type = "USER"
}

# Every member of an Identity Center group sees the records of their own queries.
resource "redshift_debug_grant" "audit_team" {
  database     = "sales_db@finance-catalog"
  grantee      = "myidc:audit_team"
  grantee_type = "ROLE"
}

# Every superuser of the producer account sees all records.
resource "redshift_debug_grant" "producer_admins" {
  database     = "sales_db@finance-catalog"
  grantee      = "123456789012"
  grantee_type = "ACCOUNT"
}
