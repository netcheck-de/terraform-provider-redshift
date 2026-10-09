SELECT COALESCE(d.description, '') AS text FROM pg_database o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_database'::regclass AND d.objsubid = 0 WHERE o.datname = :name;
