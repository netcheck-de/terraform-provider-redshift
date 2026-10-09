-- database: admin
SELECT name, type, instanceid, namespc, params, enabled FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"identity"}
