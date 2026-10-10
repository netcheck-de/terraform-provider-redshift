CREATE EXTERNAL FUNCTION "serving"."f_exfunc_upper"(integer, character varying(10), numeric(10,2), timestamp without time zone, boolean) RETURNS character varying STABLE LAMBDA 'exfunc_upper' IAM_ROLE 'arn:aws:iam::123456789012:role/lambda-udf' RETRY_TIMEOUT 0 MAX_BATCH_ROWS 100 MAX_BATCH_SIZE 512;

ALTER FUNCTION "serving"."f_exfunc_upper"(integer, character varying, numeric, timestamp without time zone, boolean) OWNER TO "admin";
