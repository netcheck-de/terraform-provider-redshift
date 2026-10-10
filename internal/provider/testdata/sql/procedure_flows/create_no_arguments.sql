-- database: admin
CREATE PROCEDURE "public"."sp_refresh"() AS $$BEGIN NULL; END;$$ LANGUAGE PLPGSQL SECURITY INVOKER;
-- params: {}

-- database: admin
SELECT p.proname AS procedure_name, u.usename AS owner, p.prosecdef AS security_definer, oidvectortypes(p.proargtypes) AS arguments, p.prosrc AS body FROM pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_user u ON u.usesysid = p.proowner WHERE n.nspname = :schema AND p.proname = :name AND p.prokind = 'p' AND p.pronargs = 0;
-- params: {"name":"sp_refresh","schema":"public"}

-- database: admin
SHOW PARAMETERS OF PROCEDURE "public"."sp_refresh"();
-- params: {}
