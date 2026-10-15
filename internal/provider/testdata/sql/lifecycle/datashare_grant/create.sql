-- database: admin
GRANT USAGE ON DATASHARE "producer" TO ACCOUNT '123456789012';
-- params: {}

-- database: admin
SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_account = :account AND NVL(consumer_namespace, '') = '';
-- params: {"account":"123456789012","share":"producer"}
