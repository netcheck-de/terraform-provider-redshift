SHOW GRANTS ON DATABASE "sales_db@finance-catalog";

GRANT DEBUG ON DATABASE "sales_db@finance-catalog" TO "IAM:sales_auditor";

REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM "IAM:sales_auditor";
