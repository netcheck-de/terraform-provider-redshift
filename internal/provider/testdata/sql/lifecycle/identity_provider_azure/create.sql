-- database: admin
CREATE IDENTITY PROVIDER "oauth_standard" TYPE azure NAMESPACE 'aad' PARAMETERS '{"issuer":"https://sts.windows.net/tenant/","client_id":"87f4aa26-78b7-410e-bf29-57b39929ef9a","client_secret":"secret","audience":["https://analysis.windows.net/powerbi/connector/AmazonRedshift"]}';
-- params: {}

-- database: admin
SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers WHERE name = :name;
-- params: {"name":"oauth_standard"}
