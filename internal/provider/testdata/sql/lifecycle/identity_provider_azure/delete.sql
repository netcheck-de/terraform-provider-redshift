-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"oauth_standard"}

-- database: admin
SELECT usename FROM pg_user WHERE LEFT(usename, LENGTH(:prefix)) = :prefix;
-- params: {"prefix":"aad:"}

-- database: admin
DROP IDENTITY PROVIDER "oauth_standard";
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"oauth_standard"}
