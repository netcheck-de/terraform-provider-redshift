-- database: admin
SELECT usename FROM pg_user WHERE LEFT(usename, LENGTH(:prefix)) = :prefix;
-- params: {"prefix":"aad:"}

-- database: admin
ALTER IDENTITY PROVIDER "oauth_standard" NAMESPACE 'entra';
-- params: {}

-- database: admin
ALTER IDENTITY PROVIDER "oauth_standard" AUTO_CREATE_ROLES TRUE INCLUDE GROUPS LIKE 'finance_%';
-- params: {}

-- database: admin
ALTER IDENTITY PROVIDER "oauth_standard" ENABLE;
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"oauth_standard"}
