SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW TEMPLATES FROM SCHEMA "admin"."serving" LIKE 'csv_load';

SHOW GRANTS ON TEMPLATE "admin"."serving"."csv_load";

GRANT USAGE ON TEMPLATE "admin"."serving"."csv_load" TO ROLE "loaders";

REVOKE USAGE ON TEMPLATE "admin"."serving"."csv_load" FROM ROLE "loaders";
