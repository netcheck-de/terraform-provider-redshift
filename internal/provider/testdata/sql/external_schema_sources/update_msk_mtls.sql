-- database: admin
SELECT n.nspname AS schemaname, s.eskind, s.databasename, s.esoptions, u.usename AS owner FROM pg_namespace n LEFT JOIN svv_external_schemas s ON s.esoid = n.oid LEFT JOIN pg_user u ON u.usesysid = n.nspowner WHERE n.nspname = :name;
-- params: {"name":"example_external"}

-- database: admin
ALTER EXTERNAL SCHEMA "example_external" URI 'b-1.example.kafka.eu-central-1.amazonaws.com:9094';
-- params: {}

-- database: admin
ALTER EXTERNAL SCHEMA "example_external" AUTHENTICATION mtls AUTHENTICATION_ARN 'arn:aws:acm:eu-central-1:123456789012:certificate/example';
-- params: {}

-- database: admin
SELECT n.nspname AS schemaname, s.eskind, s.databasename, s.esoptions, u.usename AS owner FROM pg_namespace n LEFT JOIN svv_external_schemas s ON s.esoid = n.oid LEFT JOIN pg_user u ON u.usesysid = n.nspowner WHERE n.nspname = :name;
-- params: {"name":"example_external"}
