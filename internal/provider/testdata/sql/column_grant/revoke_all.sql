SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SELECT column_name, privilege_type FROM svv_column_privileges WHERE namespace_name = :schema AND relation_name = :object AND identity_name = :grantee AND identity_type = :identity_type ORDER BY privilege_type, column_name;

REVOKE SELECT ("id", "label") ON TABLE "warehouse"."serving"."events" FROM ROLE "example:readers";

REVOKE UPDATE ("label") ON TABLE "warehouse"."serving"."events" FROM ROLE "example:readers";
