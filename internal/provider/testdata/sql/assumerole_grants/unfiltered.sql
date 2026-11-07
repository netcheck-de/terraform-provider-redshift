SELECT iam_arn, command_type AS privilege_type, identity_name, identity_type FROM svv_iam_privileges ORDER BY iam_arn, identity_type, identity_name, command_type;
