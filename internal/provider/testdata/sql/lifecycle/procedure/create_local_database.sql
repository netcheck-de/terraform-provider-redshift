-- database: warehouse
CREATE PROCEDURE "public"."sp_example"("min_id" IN integer, "total" OUT bigint) AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE plpgsql SECURITY INVOKER;
-- params: {}

-- database: warehouse
ALTER PROCEDURE "public"."sp_example"(integer) OWNER TO "admin";
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT p.proname AS procedure_name, u.usename AS owner, p.prosecdef AS security_definer, oidvectortypes(p.proargtypes) AS arguments, p.prosrc AS body FROM pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_user u ON u.usesysid = p.proowner WHERE n.nspname = :schema AND p.proname = :name AND p.prokind = 'p' AND oidvectortypes(p.proargtypes) = :arguments;
-- params: {"arguments":"integer","name":"sp_example","schema":"public"}

-- database: warehouse
SHOW PARAMETERS OF PROCEDURE "public"."sp_example"(integer);
-- params: {}
