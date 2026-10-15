SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT DISTINCT schema_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema;

SELECT privilege_type, admin_option FROM (SELECT privilege_type, CASE WHEN MIN(opt) = 1 THEN 'true' ELSE 'false' END AS admin_option, COUNT(*) AS objects FROM (SELECT relation_name, privilege_type, MAX(CASE WHEN admin_option THEN 1 ELSE 0 END) AS opt FROM svv_relation_privileges WHERE namespace_name = :schema AND identity_name = :grantee AND identity_type = LOWER(:kind) AND privilege_type IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE', 'DROP', 'REFERENCES') GROUP BY relation_name, privilege_type) AS per_object GROUP BY privilege_type) AS granted WHERE objects = (SELECT COUNT(*) FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema);

GRANT INSERT ON ALL TABLES IN SCHEMA "Odd""Schema" TO "Odd""User";

REVOKE INSERT ON ALL TABLES IN SCHEMA "Odd""Schema" FROM "Odd""User";
