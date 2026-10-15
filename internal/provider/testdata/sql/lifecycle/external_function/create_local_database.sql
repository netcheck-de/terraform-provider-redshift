-- database: warehouse
CREATE EXTERNAL FUNCTION "serving"."f_exfunc_upper"(CHARACTER VARYING) RETURNS CHARACTER VARYING STABLE LAMBDA 'exfunc_upper' IAM_ROLE 'arn:aws:iam::123456789012:role/lambda-udf';
-- params: {}

-- database: warehouse
ALTER FUNCTION "serving"."f_exfunc_upper"(CHARACTER VARYING) OWNER TO "admin";
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT n.nspname AS schema_name, p.proname AS routine_name, oidvectortypes(p.proargtypes) AS arguments, format_type(p.prorettype, NULL) AS return_type, p.provolatile AS volatility, p.prosecdef AS security_definer, l.lanname AS language, u.usename AS owner FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid JOIN pg_language l ON p.prolang = l.oid LEFT JOIN pg_user u ON p.proowner = u.usesysid WHERE n.nspname = :schema AND p.proname = :name AND oidvectortypes(p.proargtypes) = :arguments;
-- params: {"arguments":"character varying","name":"f_exfunc_upper","schema":"serving"}
