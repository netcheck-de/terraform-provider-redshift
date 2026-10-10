-- database: admin
SELECT s.schema_name, u.usename AS owner, LOWER(s.schema_type) AS schema_type, s.source_database FROM svv_all_schemas s LEFT JOIN pg_user u ON u.usesysid = s.schema_owner AND LOWER(s.schema_type) <> 'shared' WHERE s.database_name = :database AND LOWER(s.schema_type) = :schema_type ORDER BY s.schema_name;
-- params: {"database":"consumer_db","schema_type":"shared"}
