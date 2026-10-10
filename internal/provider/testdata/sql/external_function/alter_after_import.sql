CREATE OR REPLACE EXTERNAL FUNCTION "serving"."f_exfunc_upper"(character varying) RETURNS character varying STABLE LAMBDA 'exfunc_upper' IAM_ROLE 'arn:aws:iam::123456789012:role/lambda-udf';
