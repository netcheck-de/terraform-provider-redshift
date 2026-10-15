-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"oauth_standard"}
