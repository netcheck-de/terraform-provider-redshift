SELECT usename FROM pg_user WHERE LEFT(usename, LENGTH(:prefix)) = :prefix;
