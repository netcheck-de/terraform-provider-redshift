-- database: warehouse
CREATE EXTERNAL SCHEMA "example_external" FROM DATA CATALOG DATABASE 'example_glue' IAM_ROLE 'arn:aws:iam::123456789012:role/spectrum';
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT schemaname, eskind, databasename, esoptions FROM svv_external_schemas WHERE schemaname = :name;
-- params: {"name":"example_external"}
