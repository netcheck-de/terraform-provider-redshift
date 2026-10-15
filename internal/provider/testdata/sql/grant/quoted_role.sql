SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT role_name FROM svv_roles WHERE role_name = :role;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS FOR ROLE "example:Odd""Role" FROM DATABASE "Odd""Database";

GRANT USAGE ON SCHEMA "Odd""Database"."Odd""Schema" TO ROLE "example:Odd""Role";

REVOKE USAGE ON SCHEMA "Odd""Database"."Odd""Schema" FROM ROLE "example:Odd""Role";
