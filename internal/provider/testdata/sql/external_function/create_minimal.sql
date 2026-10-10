CREATE EXTERNAL FUNCTION "serving"."f_exfunc_upper"() RETURNS integer VOLATILE LAMBDA 'exfunc_upper' IAM_ROLE default;
