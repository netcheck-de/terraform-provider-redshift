-- database: admin
GRANT USAGE ON DATASHARE "producer" TO NAMESPACE '12345678-1234-1234-1234-123456789abc';
-- params: {}

-- database: admin
SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_namespace = :namespace;
-- params: {"namespace":"12345678-1234-1234-1234-123456789abc","share":"producer"}
