SELECT table_name AS member_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_type <> 'EXTERNAL TABLE';
