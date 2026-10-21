-- database: admin
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}

-- database: admin
REVOKE DEBUG ON DATABASE "warehouse" FROM ADMINS OF ACCOUNT '123456789012';
-- params: {}

-- database: admin
SHOW GRANTS ON DATABASE "warehouse";
-- params: {}
