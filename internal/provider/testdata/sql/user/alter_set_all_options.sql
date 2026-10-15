ALTER USER "Odd""User" SYSLOG ACCESS UNRESTRICTED;

ALTER USER "Odd""User" VALID UNTIL '2030-06-30 21:59:00+00';

ALTER USER "Odd""User" CONNECTION LIMIT 10;

ALTER USER "Odd""User" SESSION TIMEOUT 300;

ALTER USER "Odd""User" SET search_path TO '$user', 'Odd"Schema', 'it''s \\path';

ALTER USER "Odd""User" SET query_group TO 'it''s \\x';

ALTER USER "Odd""User" SET timezone TO 'Europe/Berlin';
