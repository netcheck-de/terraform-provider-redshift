-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT polname, relschema, relname, grantee, granteekind FROM svv_rls_attached_policy WHERE polname = :policy AND relschema = :schema AND relname = :relation AND grantee = :grantee AND granteekind = :kind;
-- params: {"grantee":"analysts","kind":"role","policy":"region_filter","relation":"events","schema":"public"}
