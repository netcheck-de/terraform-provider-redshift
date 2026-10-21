# Remote USER/ROLE aliases require SQL-superuser access to a current-session IAM AssumeUser authentication event,
# plus SHOW DATABASES visibility. Password/local-secret sessions cannot verify remote DEBUG readback.
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

# Connect this resource's provider to the producer warehouse; remote ACCOUNT readback is unavailable.
# Every superuser of the producer account sees all records.
resource "redshift_debug_grant" "producer_admins" {
  database     = "sales_db"
  grantee      = "123456789012"
  grantee_type = "ACCOUNT"
}
