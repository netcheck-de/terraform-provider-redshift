SHOW GRANTS FOR "IAMR:audit_role" FROM DATABASE "sales_db@finance-catalog";

GRANT DEBUG ON DATABASE "sales_db@finance-catalog" TO "IAMR:audit_role";

REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM "IAMR:audit_role";
