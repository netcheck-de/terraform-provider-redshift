-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"analytics"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"analytics","schema":"public"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"analytics","name":"masking_exempt","schema":"public"}

-- database: admin
SELECT policy_name FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;
-- params: {"database":"analytics","name":"mask_email"}
