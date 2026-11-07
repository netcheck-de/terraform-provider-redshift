-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: admin
SELECT schema_name, table_name, table_owner FROM svv_redshift_tables WHERE database_name = :database AND table_type = 'TABLE' AND schema_name !~ '^pg_' AND schema_name <> 'information_schema' AND schema_name = :schema ORDER BY schema_name, table_name;
-- params: {"database":"analytics","schema":"staging"}
