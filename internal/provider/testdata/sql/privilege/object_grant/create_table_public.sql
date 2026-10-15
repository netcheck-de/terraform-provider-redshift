-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"table","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON TABLE "warehouse"."serving"."table";
-- params: {}

-- database: warehouse
GRANT SELECT ON TABLE "warehouse"."serving"."table" TO PUBLIC;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"warehouse","schema":"serving"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"table","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON TABLE "warehouse"."serving"."table";
-- params: {}
