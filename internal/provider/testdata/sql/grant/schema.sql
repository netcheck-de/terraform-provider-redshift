SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT role_name FROM svv_roles WHERE role_name = :role;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS FOR ROLE "readers" FROM DATABASE "analytics";

GRANT CREATE ON SCHEMA "analytics"."serving" TO ROLE "readers";

REVOKE CREATE ON SCHEMA "analytics"."serving" FROM ROLE "readers";
