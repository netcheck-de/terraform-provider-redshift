-- database: admin
SELECT d.database_name, u.usename AS owner, LOWER(d.database_type) AS database_type, d.database_isolation_level AS isolation_level FROM svv_redshift_databases d LEFT JOIN pg_user u ON u.usesysid = d.database_owner WHERE d.database_name LIKE :name_like ORDER BY d.database_name;
-- params: {"name_like":"it's\\_\"%"}
