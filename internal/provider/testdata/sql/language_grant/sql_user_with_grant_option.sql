SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;

GRANT USAGE ON LANGUAGE SQL TO "analyst";

REVOKE USAGE ON LANGUAGE SQL FROM "analyst";

GRANT USAGE ON LANGUAGE SQL TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR USAGE ON LANGUAGE SQL FROM "analyst";
