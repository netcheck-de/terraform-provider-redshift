-- database: admin
CREATE IDENTITY PROVIDER "identity" TYPE AWSIDC NAMESPACE 'example' APPLICATION_ARN 'application' IAM_ROLE 'role-one';
-- params: {}

-- database: admin
ALTER IDENTITY PROVIDER "identity" DISABLE;
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
