SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND LOWER(schema_type) = 'local' ORDER BY schema_name;
