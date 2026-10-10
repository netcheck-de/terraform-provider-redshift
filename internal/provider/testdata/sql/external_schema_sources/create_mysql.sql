-- database: admin
CREATE EXTERNAL SCHEMA "example_external" FROM MYSQL DATABASE 'orders' URI 'aurora.example.internal' PORT 5432 IAM_ROLE 'arn:aws:iam::123456789012:role/federated' SECRET_ARN 'arn:aws:secretsmanager:eu-central-1:123456789012:secret:aurora-AbCdEf';
-- params: {}

-- database: admin
SELECT n.nspname AS schemaname, s.eskind, s.databasename, s.esoptions, u.usename AS owner FROM pg_namespace n LEFT JOIN svv_external_schemas s ON s.esoid = n.oid LEFT JOIN pg_user u ON u.usesysid = n.nspowner WHERE n.nspname = :name;
-- params: {"name":"example_external"}
