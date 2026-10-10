-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"Odd\"O'Reilly\\User"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"Odd\"O'Reilly\\User","identity_type":"user","language":"sql"}

-- database: warehouse
GRANT USAGE ON LANGUAGE SQL TO "Odd""O'Reilly\User" WITH GRANT OPTION;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"Odd\"O'Reilly\\User"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;
-- params: {"grantee":"Odd\"O'Reilly\\User","identity_type":"user","language":"sql"}
