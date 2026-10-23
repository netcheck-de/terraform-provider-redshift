SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "shared""db"."serving";

GRANT USAGE ON SCHEMA "shared""db"."serving" TO "reader";

REVOKE USAGE ON SCHEMA "shared""db"."serving" FROM "reader";
