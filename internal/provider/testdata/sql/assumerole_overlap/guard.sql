SELECT iam_arn FROM svv_iam_privileges WHERE iam_arn = :all_arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
