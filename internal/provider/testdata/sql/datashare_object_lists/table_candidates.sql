SELECT schema_name, table_name AS member_name FROM svv_all_tables WHERE database_name = :database AND table_type <> 'EXTERNAL TABLE';
