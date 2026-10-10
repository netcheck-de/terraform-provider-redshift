-- database: admin
SELECT c.relname FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid WHERE n.nspname = :schema AND c.relname = :relation AND c.relkind IN ('r', 'v');
-- params: {"relation":"events","schema":"public"}

-- database: admin
SELECT is_rls_on, is_rls_datashare_on, rls_conjunction_type FROM svv_rls_relation WHERE datname = :database AND relschema = :schema AND relname = :relation;
-- params: {"database":"admin","relation":"events","schema":"public"}
