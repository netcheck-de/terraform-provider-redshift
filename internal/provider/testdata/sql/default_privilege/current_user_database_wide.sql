SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = current_user AND (schema_name IS NULL OR schema_name = '') AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);

ALTER DEFAULT PRIVILEGES GRANT INSERT ON TABLES TO ROLE "readers";

ALTER DEFAULT PRIVILEGES REVOKE INSERT ON TABLES FROM ROLE "readers";
