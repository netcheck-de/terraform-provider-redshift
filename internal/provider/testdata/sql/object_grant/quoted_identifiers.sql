SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;

SHOW GRANTS ON TABLE "Odd""Database"."Odd""Schema"."Odd""Table";

GRANT SELECT ON TABLE "Odd""Database"."Odd""Schema"."Odd""Table" TO ROLE "Odd""Role";

REVOKE SELECT ON TABLE "Odd""Database"."Odd""Schema"."Odd""Table" FROM ROLE "Odd""Role";
