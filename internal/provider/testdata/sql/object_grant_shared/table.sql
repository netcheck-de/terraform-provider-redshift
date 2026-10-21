SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SHOW GRANTS ON TABLE "shared""db"."serving"."events";

GRANT SELECT ON TABLE "shared""db"."serving"."events" TO "reader";

REVOKE SELECT ON TABLE "shared""db"."serving"."events" FROM "reader";
