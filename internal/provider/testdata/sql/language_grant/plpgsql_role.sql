SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;

GRANT USAGE ON LANGUAGE PLPGSQL TO ROLE "example:readers";

REVOKE USAGE ON LANGUAGE PLPGSQL FROM ROLE "example:readers";
