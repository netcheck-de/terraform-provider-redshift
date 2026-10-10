SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND consumer_account = :account AND NVL(consumer_namespace, '') = '';

GRANT USAGE ON DATASHARE "Odd""Share" TO ACCOUNT '123456789012' VIA DATA CATALOG;

REVOKE USAGE ON DATASHARE "Odd""Share" FROM ACCOUNT '123456789012' VIA DATA CATALOG;
