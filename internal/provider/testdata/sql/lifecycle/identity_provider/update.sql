-- database: admin
ALTER IDENTITY PROVIDER "identity" IAM_ROLE 'role-one';
-- params: {}

-- database: admin
ALTER IDENTITY PROVIDER "identity" ENABLE;
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
