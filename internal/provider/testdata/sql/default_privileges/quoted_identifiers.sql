SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT usename FROM pg_user WHERE usename = :name;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);

ALTER DEFAULT PRIVILEGES FOR USER "Odd""Owner" IN SCHEMA "Odd""Schema" GRANT SELECT ON TABLES TO ROLE "Odd""Role";

ALTER DEFAULT PRIVILEGES FOR USER "Odd""Owner" IN SCHEMA "Odd""Schema" REVOKE SELECT ON TABLES FROM ROLE "Odd""Role";
