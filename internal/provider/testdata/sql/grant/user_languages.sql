SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SHOW GRANTS ON DATABASE "analytics" FOR "analyst";

GRANT USAGE FOR LANGUAGES IN DATABASE "analytics" TO "analyst";

REVOKE USAGE FOR LANGUAGES IN DATABASE "analytics" FROM "analyst";

GRANT USAGE FOR LANGUAGES IN DATABASE "analytics" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION USAGE FOR LANGUAGES IN DATABASE "analytics" FROM "analyst";
