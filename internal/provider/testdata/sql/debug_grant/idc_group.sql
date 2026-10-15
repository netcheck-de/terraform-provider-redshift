SHOW GRANTS ON DATABASE "sales_db@finance-catalog";

GRANT DEBUG ON DATABASE "sales_db@finance-catalog" TO ROLE "myidc:audit_team";

REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM ROLE "myidc:audit_team";
