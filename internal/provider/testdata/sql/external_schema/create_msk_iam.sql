CREATE EXTERNAL SCHEMA "stream" FROM MSK IAM_ROLE 'arn:aws:iam::123456789012:role/msk' AUTHENTICATION iam URI 'b-1.example.kafka.eu-central-1.amazonaws.com:9098';
