-- database: admin
ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") USING ("email") TO ROLE "example_readers" PRIORITY 10;
-- params: {}

-- database: admin
SELECT policy_name, schema_name, table_name, grantee, grantee_type, priority, input_columns, output_columns FROM svv_attached_masking_policy WHERE policy_name = :policy AND schema_name = :schema AND table_name = :relation;
-- params: {"policy":"mask_email","relation":"customers","schema":"public"}
