-- database: warehouse
CREATE EXTERNAL MODEL "serving"."summarizer" FUNCTION "serving"."f_summarize" IAM_ROLE DEFAULT MODEL_TYPE BEDROCK SETTINGS (MODEL_ID 'anthropic.claude-v2', PROMPT 'Summarize the following text:', REQUEST_TYPE UNIFIED, RESPONSE_TYPE VARCHAR);
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SHOW MODEL ALL;
-- params: {}

-- database: warehouse
SHOW MODEL "serving"."summarizer";
-- params: {}
