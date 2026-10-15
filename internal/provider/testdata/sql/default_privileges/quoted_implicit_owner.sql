SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT usename FROM pg_user WHERE usename = :name;

SELECT privilege_type, admin_option FROM (SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = 'FUNCTION' AND grantee_type = 'public' UNION ALL SELECT 'EXECUTE' AS privilege_type, false AS admin_option FROM pg_user u WHERE u.usename = :owner AND NOT EXISTS (SELECT 1 FROM pg_default_acl d WHERE d.defacluser = u.usesysid AND d.defaclnamespace = 0 AND d.defaclobjtype = 'f')) AS defaults;

ALTER DEFAULT PRIVILEGES FOR USER "Odd""Owner" GRANT EXECUTE ON FUNCTIONS TO PUBLIC;

ALTER DEFAULT PRIVILEGES FOR USER "Odd""Owner" REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
