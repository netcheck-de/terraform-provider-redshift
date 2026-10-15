SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SHOW GRANTS ON DATABASE "analytics" FOR "analyst";

GRANT CREATE ON DATABASE "analytics" TO "analyst";

REVOKE CREATE ON DATABASE "analytics" FROM "analyst";

GRANT CREATE ON DATABASE "analytics" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR CREATE ON DATABASE "analytics" FROM "analyst";
