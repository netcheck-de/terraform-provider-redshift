-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}

-- database: admin
REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM "IAM:sales_auditor";
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}
