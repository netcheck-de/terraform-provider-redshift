-- database: warehouse
ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") TO ROLE "example_readers" PRIORITY 10;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT policy_name, schema_name, table_name, grantee, grantee_type, priority, input_columns, output_columns FROM svv_attached_masking_policy WHERE policy_name = :policy AND schema_name = :schema AND table_name = :relation;
-- params: {"policy":"mask_email","relation":"customers","schema":"public"}
