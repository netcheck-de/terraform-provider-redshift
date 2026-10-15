SELECT consumer_database FROM svv_datashares WHERE share_type = 'INBOUND' AND share_name = :share AND producer_account = :account AND producer_namespace = :namespace;
