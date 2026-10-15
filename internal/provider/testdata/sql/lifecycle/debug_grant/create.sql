-- database: admin
GRANT DEBUG ON DATABASE "sales_db@finance-catalog" TO "IAM:sales_auditor";
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}
