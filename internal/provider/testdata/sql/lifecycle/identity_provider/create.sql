-- database: admin
CREATE IDENTITY PROVIDER "identity" TYPE AWSIDC NAMESPACE 'example' APPLICATION_ARN 'application' IAM_ROLE 'role-one';
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
