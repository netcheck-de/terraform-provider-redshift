SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SELECT polname FROM svv_rls_policy WHERE poldb = :database AND polname = :name;

GRANT SELECT ON TABLE "analytics"."public"."masking_exempt" TO RLS POLICY "policy_concerts";

REVOKE SELECT ON TABLE "analytics"."public"."masking_exempt" FROM RLS POLICY "policy_concerts";
