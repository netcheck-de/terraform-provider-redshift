-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}

-- database: admin
REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM ROLE "myidc:audit_team";
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}
