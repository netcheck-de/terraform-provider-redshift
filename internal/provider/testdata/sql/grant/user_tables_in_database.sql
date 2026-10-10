SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SHOW GRANTS ON DATABASE "analytics" FOR "analyst";

GRANT SELECT FOR TABLES IN DATABASE "analytics" TO "analyst";

REVOKE SELECT FOR TABLES IN DATABASE "analytics" FROM "analyst";

GRANT SELECT FOR TABLES IN DATABASE "analytics" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION SELECT FOR TABLES IN DATABASE "analytics" FROM "analyst";
