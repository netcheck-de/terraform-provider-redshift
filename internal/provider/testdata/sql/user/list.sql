SELECT usename, usesuper, usecreatedb, valuntil, array_to_string(useconfig, chr(30)) AS useconfig FROM pg_user ORDER BY usename;
