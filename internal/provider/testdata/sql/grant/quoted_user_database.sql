SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SHOW GRANTS ON DATABASE "Odd""Database" FOR "Odd""User";

GRANT USAGE ON DATABASE "Odd""Database" TO "Odd""User";

REVOKE USAGE ON DATABASE "Odd""Database" FROM "Odd""User";

GRANT USAGE ON DATABASE "Odd""Database" TO "Odd""User" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR USAGE ON DATABASE "Odd""Database" FROM "Odd""User";
