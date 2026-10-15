SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "serving";

GRANT SELECT FOR TABLES IN SCHEMA "serving" TO DATASHARE "producer";

REVOKE SELECT FOR TABLES IN SCHEMA "serving" FROM DATASHARE "producer";
