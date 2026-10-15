SELECT quota FROM svv_redshift_schema_quota WHERE TRIM(database_name) = :database AND TRIM(schema_name) = :name;
