-- database: admin
SELECT policy_database, policy_name, input_columns, policy_expression FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;
-- params: {"database":"admin","name":"mask_email"}

-- database: admin
DROP MASKING POLICY "mask_email";
-- params: {}

-- database: admin
SELECT policy_database, policy_name, input_columns, policy_expression FROM svv_masking_policy WHERE policy_database = :database AND policy_name = :name;
-- params: {"database":"admin","name":"mask_email"}
