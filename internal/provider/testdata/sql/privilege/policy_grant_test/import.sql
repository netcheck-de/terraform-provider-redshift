-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"warehouse","name":"regions","schema":"serving"}

-- database: warehouse
SHOW GRANTS ON TABLE "warehouse"."serving"."regions";
-- params: {}
