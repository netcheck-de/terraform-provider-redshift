-- database: admin
ATTACH RLS POLICY "region_filter" ON "public"."events" TO "loader";
-- params: {}

-- database: admin
SELECT polname, relschema, relname, grantee, granteekind FROM svv_rls_attached_policy WHERE polname = :policy AND relschema = :schema AND relname = :relation AND grantee = :grantee AND granteekind = :kind;
-- params: {"grantee":"loader","kind":"user","policy":"region_filter","relation":"events","schema":"public"}
