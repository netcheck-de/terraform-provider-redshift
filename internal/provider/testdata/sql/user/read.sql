SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
