-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}

-- database: admin
REVOKE DEBUG ON DATABASE "sales_db@finance-catalog" FROM ADMINS OF ACCOUNT '123456789012';
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "sales_db@finance-catalog";
-- params: {}
