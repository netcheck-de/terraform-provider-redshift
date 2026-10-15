SHOW GRANTS ON DATABASE "sales_db";

GRANT DEBUG ON DATABASE "sales_db" TO "IAM:sales_auditor";

REVOKE DEBUG ON DATABASE "sales_db" FROM "IAM:sales_auditor";
