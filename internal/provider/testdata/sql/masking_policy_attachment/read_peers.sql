SELECT policy_name, grantee, grantee_type, priority, output_columns FROM svv_attached_masking_policy WHERE schema_name = :schema AND table_name = :relation;
