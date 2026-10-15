-- database: admin
SELECT t.schema_name, t.table_name, UPPER(t.table_type) AS table_type, r.table_owner AS owner, t.remarks FROM svv_all_tables t LEFT JOIN svv_redshift_tables r ON r.database_name = t.database_name AND r.schema_name = t.schema_name AND r.table_name = t.table_name WHERE t.database_name = :database AND UPPER(t.table_type) = :table_type ORDER BY t.schema_name, t.table_name;
-- params: {"database":"analytics","table_type":"TABLE"}
