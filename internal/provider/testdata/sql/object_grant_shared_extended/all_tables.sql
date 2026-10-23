SELECT usesuper FROM pg_user WHERE usename = current_user;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT DISTINCT table_name AS object_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema ORDER BY object_name;

SHOW GRANTS ON TABLE "shared_db"."serving"."member";

GRANT SELECT ON ALL TABLES IN SCHEMA "shared_db"."serving" TO "reader" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR SELECT ON ALL TABLES IN SCHEMA "shared_db"."serving" FROM "reader";

REVOKE SELECT ON ALL TABLES IN SCHEMA "shared_db"."serving" FROM "reader";
