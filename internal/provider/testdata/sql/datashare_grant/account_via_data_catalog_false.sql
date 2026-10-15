SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_account = :account AND NVL(consumer_namespace, '') = '';

GRANT USAGE ON DATASHARE "producer" TO ACCOUNT '123456789012';

REVOKE USAGE ON DATASHARE "producer" FROM ACCOUNT '123456789012';
