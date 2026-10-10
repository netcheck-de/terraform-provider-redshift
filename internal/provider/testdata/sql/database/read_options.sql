SELECT u.usename AS owner, d.datconnlimit AS connection_limit FROM pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba WHERE d.datname = :name;
