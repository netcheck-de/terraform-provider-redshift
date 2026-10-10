-- database: admin
CREATE FUNCTION "public"."f_example"(integer) RETURNS integer IMMUTABLE AS $$SELECT $1 + 1$$ LANGUAGE sql;
-- params: {}

-- database: admin
ALTER FUNCTION "public"."f_example"(integer) OWNER TO "admin";
-- params: {}

-- database: admin
SELECT p.proname AS function_name, u.usename AS owner, l.lanname AS language, p.provolatile AS volatility, format_type(p.prorettype, NULL) AS return_type, oidvectortypes(p.proargtypes) AS arguments, p.prosrc AS body FROM pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_language l ON l.oid = p.prolang JOIN pg_user u ON u.usesysid = p.proowner WHERE n.nspname = :schema AND p.proname = :name AND p.prokind = 'f' AND oidvectortypes(p.proargtypes) = :arguments;
-- params: {"arguments":"integer","name":"f_example","schema":"public"}
