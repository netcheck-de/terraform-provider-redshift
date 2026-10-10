SELECT autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database AND RTRIM(schema_name) = :schema AND RTRIM(name) = :name;
