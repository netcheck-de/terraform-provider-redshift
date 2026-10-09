-- database: admin
SELECT name, type, instanceid, namespc, params, enabled FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}

-- database: admin
SELECT usename FROM pg_user WHERE LEFT(usename, LENGTH(:prefix)) = :prefix;
-- params: {"prefix":"example:"}

-- database: admin
DROP IDENTITY PROVIDER "identity";
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
