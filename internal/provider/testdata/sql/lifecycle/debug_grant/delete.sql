-- database: admin
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}

-- database: admin
REVOKE DEBUG ON DATABASE "warehouse" FROM "IAM:sales_auditor";
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}
