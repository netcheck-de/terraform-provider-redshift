SELECT TRIM(schema_name) AS schema_name, quota FROM svv_redshift_schema_quota WHERE TRIM(database_name) = :database;
