-- database: admin
SELECT polname, relschema, relname, grantee, granteekind FROM svv_rls_attached_policy WHERE polname = :policy AND relschema = :schema AND relname = :relation AND grantee = :grantee AND granteekind = :kind;
-- params: {"grantee":"loader","kind":"user","policy":"region_filter","relation":"events","schema":"public"}

-- database: admin
DETACH RLS POLICY "region_filter" ON "public"."events" FROM "loader";
-- params: {}

-- database: admin
SELECT polname, relschema, relname, grantee, granteekind FROM svv_rls_attached_policy WHERE polname = :policy AND relschema = :schema AND relname = :relation AND grantee = :grantee AND granteekind = :kind;
-- params: {"grantee":"loader","kind":"user","policy":"region_filter","relation":"events","schema":"public"}
