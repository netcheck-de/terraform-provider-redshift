SHOW GRANTS FOR ROLE "myidc:audit_team" FROM DATABASE "sales_db@finance-catalog";

GRANT DEBUG ON DATABASE "sales_db@finance-catalog" TO ROLE "myidc:audit_team";

REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM ROLE "myidc:audit_team";
