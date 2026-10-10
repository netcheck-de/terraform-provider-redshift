-- database: admin
CREATE DATABASE "warehouse";
-- params: {}

-- database: admin
SHOW DATABASES LIKE 'warehouse';
-- params: {}

-- database: admin
SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
-- params: {"name":"warehouse"}

-- database: warehouse
SELECT db_collation() AS collation;
-- params: {}
