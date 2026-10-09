SELECT groname FROM pg_group WHERE groname = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SHOW GRANTS ON TABLE "warehouse"."serving"."orders";

GRANT SELECT ON TABLE "warehouse"."serving"."orders" TO GROUP "readers";

REVOKE SELECT ON TABLE "warehouse"."serving"."orders" FROM GROUP "readers";
