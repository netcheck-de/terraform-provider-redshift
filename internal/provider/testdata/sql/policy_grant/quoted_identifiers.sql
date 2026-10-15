SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SELECT policy_name FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;

GRANT SELECT ON TABLE "Odd""Database"."Odd""Schema"."It's\Table" TO MASKING POLICY "Odd""Policy";

REVOKE SELECT ON TABLE "Odd""Database"."Odd""Schema"."It's\Table" FROM MASKING POLICY "Odd""Policy";
