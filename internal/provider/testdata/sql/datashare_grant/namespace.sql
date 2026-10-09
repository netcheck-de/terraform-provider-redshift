SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_namespace = :namespace;

GRANT USAGE ON DATASHARE "producer" TO NAMESPACE '12345678-1234-1234-1234-123456789abc';

REVOKE USAGE ON DATASHARE "producer" FROM NAMESPACE '12345678-1234-1234-1234-123456789abc';
