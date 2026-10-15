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
SELECT polname FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"analytics","name":"mask_email"}

-- database: analytics
GRANT SELECT ON TABLE "analytics"."public"."masking_exempt" TO RLS POLICY "mask_email";
-- params: {}
