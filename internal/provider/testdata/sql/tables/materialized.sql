SELECT TRIM(schema_name) AS schema_name, TRIM(name) AS name FROM svv_mv_info WHERE TRIM(database_name) = :database;
