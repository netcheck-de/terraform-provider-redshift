-- database: admin
SELECT schema_name, table_name, column_name, ordinal_position, data_type, character_maximum_length, numeric_precision, numeric_scale, UPPER(is_nullable) AS is_nullable, column_default, remarks FROM svv_all_columns WHERE database_name = :database ORDER BY schema_name, table_name, ordinal_position;
-- params: {"database":"analytics"}
