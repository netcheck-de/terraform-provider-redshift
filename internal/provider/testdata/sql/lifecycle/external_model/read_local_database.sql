-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SHOW MODEL ALL;
-- params: {}

-- database: warehouse
SHOW MODEL "serving"."summarizer";
-- params: {}
