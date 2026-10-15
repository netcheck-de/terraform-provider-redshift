SELECT column_name, column_default, encoding, distkey, sortkey FROM svv_redshift_columns WHERE database_name = :database AND schema_name = :schema AND table_name = :name ORDER BY ordinal_position;
