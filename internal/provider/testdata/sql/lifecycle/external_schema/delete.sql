-- database: admin
SELECT schemaname, eskind, databasename, esoptions FROM svv_external_schemas WHERE schemaname = :name;
-- params: {"name":"example_external"}

-- database: admin
DROP SCHEMA "example_external";
-- params: {}

-- database: admin
SELECT schemaname, eskind, databasename, esoptions FROM svv_external_schemas WHERE schemaname = :name;
-- params: {"name":"example_external"}
