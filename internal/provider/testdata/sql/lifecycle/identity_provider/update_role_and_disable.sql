-- database: admin
ALTER IDENTITY PROVIDER "identity" IAM_ROLE 'role-two';
-- params: {}

-- database: admin
ALTER IDENTITY PROVIDER "identity" DISABLE;
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
