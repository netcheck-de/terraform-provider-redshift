-- database: admin
SHOW DATABASES LIKE 'warehouse';
-- params: {}

-- database: admin
SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
-- params: {"name":"warehouse"}

-- database: admin
ALTER DATABASE "warehouse" OWNER TO "Etl""Owner";
-- params: {}

-- database: admin
ALTER DATABASE "warehouse" CONNECTION LIMIT 25;
-- params: {}

-- database: admin
ALTER DATABASE "warehouse" ISOLATION LEVEL SERIALIZABLE;
-- params: {}

-- database: admin
SHOW DATABASES LIKE 'warehouse';
-- params: {}

-- database: admin
SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
-- params: {"name":"warehouse"}
