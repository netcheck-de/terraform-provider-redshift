CREATE OR REPLACE EXTERNAL FUNCTION "Odd""Schema"."F""Upper"(character varying) RETURNS character varying STABLE LAMBDA 'it''s \\lambda' IAM_ROLE 'arn:aws:iam::123456789012:role/lambda-udf';

ALTER FUNCTION "Odd""Schema"."F""Upper"(character varying) OWNER TO "Odd""Owner";
