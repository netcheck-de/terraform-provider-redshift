SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = current_user AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);

ALTER DEFAULT PRIVILEGES IN SCHEMA "serving" GRANT SELECT ON TABLES TO ROLE "readers";

ALTER DEFAULT PRIVILEGES IN SCHEMA "serving" REVOKE SELECT ON TABLES FROM ROLE "readers";
