SELECT polname, relschema, relname, grantee, granteekind FROM svv_rls_attached_policy ORDER BY relschema, relname, polname, granteekind, grantee;
