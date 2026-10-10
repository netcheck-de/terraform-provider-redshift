SELECT groname FROM pg_group WHERE groname = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT privilege_type, admin_option FROM svv_language_privileges WHERE language_name = :language AND identity_name = :grantee AND identity_type = :identity_type;

GRANT USAGE ON LANGUAGE sql TO GROUP "udf_devs";

REVOKE USAGE ON LANGUAGE sql FROM GROUP "udf_devs";
