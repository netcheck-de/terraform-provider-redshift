SELECT TRIM(schema_name) AS schema_name, TRIM(name) AS name, TRIM(user_name) AS owner FROM svv_mv_info WHERE TRIM(database_name) = :database AND TRIM(schema_name) = :schema;
