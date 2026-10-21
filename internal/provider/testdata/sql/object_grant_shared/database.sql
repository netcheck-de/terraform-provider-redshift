SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SHOW GRANTS ON DATABASE "shared""db";

GRANT USAGE ON DATABASE "shared""db" TO "reader";

REVOKE USAGE ON DATABASE "shared""db" FROM "reader";
