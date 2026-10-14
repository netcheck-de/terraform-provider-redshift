SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW TEMPLATES FROM SCHEMA "Odd""Schema" LIKE 'odd_name_%';

SHOW GRANTS ON TEMPLATE "Odd""Database"."Odd""Schema"."odd\name_%";

GRANT USAGE ON TEMPLATE "Odd""Database"."Odd""Schema"."odd\name_%" TO ROLE "Odd""Role";

REVOKE USAGE ON TEMPLATE "Odd""Database"."Odd""Schema"."odd\name_%" FROM ROLE "Odd""Role";
