SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "Odd""Database"."Odd""Schema" FOR "Odd""User";

GRANT SELECT FOR TABLES IN SCHEMA "Odd""Schema" DATABASE "Odd""Database" TO "Odd""User";

REVOKE SELECT FOR TABLES IN SCHEMA "Odd""Schema" DATABASE "Odd""Database" FROM "Odd""User";

GRANT SELECT FOR TABLES IN SCHEMA "Odd""Schema" DATABASE "Odd""Database" TO "Odd""User" WITH GRANT OPTION;

REVOKE GRANT OPTION SELECT FOR TABLES IN SCHEMA "Odd""Schema" DATABASE "Odd""Database" FROM "Odd""User";
