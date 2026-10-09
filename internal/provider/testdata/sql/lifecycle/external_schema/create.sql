-- database: admin
CREATE EXTERNAL SCHEMA "example_external" FROM DATA CATALOG DATABASE 'example_glue' IAM_ROLE 'arn:aws:iam::123456789012:role/spectrum';
-- params: {}

-- database: admin
SELECT schemaname, eskind, databasename, esoptions FROM svv_external_schemas WHERE schemaname = :name;
-- params: {"name":"example_external"}
