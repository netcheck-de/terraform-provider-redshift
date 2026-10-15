-- database: admin
CREATE OR REPLACE PROCEDURE "public"."sp_example"("min_id" IN INTEGER, "total" OUT BIGINT) NONATOMIC AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE PLPGSQL;
-- params: {}

-- database: admin
SELECT p.proname AS procedure_name, u.usename AS owner, p.prosecdef AS security_definer, oidvectortypes(p.proargtypes) AS arguments, p.prosrc AS body FROM pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_user u ON u.usesysid = p.proowner WHERE n.nspname = :schema AND p.proname = :name AND p.prokind = 'p' AND oidvectortypes(p.proargtypes) = :arguments;
-- params: {"arguments":"integer","name":"sp_example","schema":"public"}

-- database: admin
SHOW PARAMETERS OF PROCEDURE "public"."sp_example"(INTEGER);
-- params: {}
