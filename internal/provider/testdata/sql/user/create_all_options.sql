CREATE USER "Odd""User" PASSWORD 'it''s \\secret' NOCREATEUSER NOCREATEDB SYSLOG ACCESS UNRESTRICTED VALID UNTIL '2030-06-30 21:59:00+00' CONNECTION LIMIT 10 SESSION TIMEOUT 300;

ALTER USER "Odd""User" SET search_path TO '$user', 'Odd"Schema', 'it''s \\path';

ALTER USER "Odd""User" SET query_group TO 'it''s \\x';

ALTER USER "Odd""User" SET timezone TO 'Europe/Berlin';
