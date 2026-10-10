-- database: warehouse
CREATE MASKING POLICY "mask_email" WITH ("email" CHARACTER VARYING(256)) USING ('***'::VARCHAR(256));
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT policy_database, policy_name, input_columns, policy_expression FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;
-- params: {"database":"warehouse","name":"mask_email"}
