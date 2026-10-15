-- database: admin
CREATE MASKING POLICY "Odd""Policy" WITH ("email" CHARACTER VARYING(256)) USING ('it''s \ masked'::VARCHAR(256));
-- params: {}

-- database: admin
SELECT policy_database, policy_name, input_columns, policy_expression FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;
-- params: {"database":"admin","name":"Odd\"Policy"}
