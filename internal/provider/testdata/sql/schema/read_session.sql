SELECT usename AS name, usesuper AS superuser FROM pg_user WHERE usename = current_user;
