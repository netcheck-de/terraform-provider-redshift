SELECT values, location FROM svv_external_partitions WHERE LOWER(schemaname) = LOWER(:schema) AND LOWER(tablename) = LOWER(:table);
