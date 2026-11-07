SELECT RTRIM(schema_name) AS schema_name, RTRIM(name) AS name, autorefresh FROM svv_mv_info WHERE RTRIM(database_name) = :database;
