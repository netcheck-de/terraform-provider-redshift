-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"odd\"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"odd\"database"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"odd\"database","schema":"odd\"schema"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"odd\"database","name":"odd\"table","schema":"odd\"schema"}

-- database: odd"database
SHOW GRANTS ON TABLE "odd""database"."odd""schema"."odd""table";
-- params: {}

-- database: odd"database
GRANT SELECT ON TABLE "odd""database"."odd""schema"."odd""table" TO GROUP "odd""readers";
-- params: {}

-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"odd\"readers"}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"odd\"database"}

-- database: admin
SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;
-- params: {"database":"odd\"database","schema":"odd\"schema"}

-- database: admin
SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name;
-- params: {"database":"odd\"database","name":"odd\"table","schema":"odd\"schema"}

-- database: odd"database
SHOW GRANTS ON TABLE "odd""database"."odd""schema"."odd""table";
-- params: {}
