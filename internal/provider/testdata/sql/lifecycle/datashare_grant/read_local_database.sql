-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_account = :account AND NVL(consumer_namespace, '') = '';
-- params: {"account":"123456789012","share":"producer"}
