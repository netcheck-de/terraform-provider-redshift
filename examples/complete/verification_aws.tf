# Independent AWS reads are deliberately downstream of provisioning. Do not use these lookups in provider
# configuration: the SQL providers must remain usable while the rest of this verification graph is constructed.
# Postconditions fail the apply (unlike advisory check blocks). These are service readbacks, not permission simulation.
#
# AWS provider 6.67 has no lookups for inline IAM policies, Redshift parameter groups, inbound/DynamoDB/Glue resource
# policies, Redshift IDC applications, or either zero-ETL integration type. Those remain provider-refresh/waiter based;
# the SQL integration lookup separately verifies the destination binding, not AWS integration status. Workgroup
# capacity/config parameters, RDS replication parameter values, and S3 bucket encryption/public-access settings are
# also absent from these lookups. Endpoint-group rule contents and directory membership remain provider-refresh based.
# Existing RDS/Aurora source settings are operator-managed prerequisites, not verified by this owned-fixture readback.
# Data-share authorization/association and COPY notification integration remain resource-refresh based; the shared SQL
# row probe verifies sharing end to end, but neither a bucket policy nor a share ARN proves AWS integration readiness.
# The existing SQL probes exercise managed administrator credentials; do not duplicate their passwords in data state.

data "aws_vpc" "verify_producer" {
  provider = aws.producer
  id       = module.producer_vpc.vpc_id

  lifecycle {
    postcondition {
      condition = (
        self.cidr_block == var.producer_vpc_cidr && self.owner_id == data.aws_caller_identity.producer.account_id &&
        self.enable_dns_support && self.enable_dns_hostnames
      )
      error_message = "The producer VPC must have the requested CIDR, account ownership, and endpoint DNS support."
    }
  }
}

data "aws_vpc" "verify_consumer" {
  provider = aws.consumer
  id       = module.consumer_vpc.vpc_id

  lifecycle {
    postcondition {
      condition = (
        self.cidr_block == var.consumer_vpc_cidr && self.owner_id == data.aws_caller_identity.consumer.account_id &&
        self.enable_dns_support && self.enable_dns_hostnames
      )
      error_message = "The consumer VPC must have the requested CIDR, account ownership, and endpoint DNS support."
    }
  }
}

# Index keys are known before apply, unlike the subnet IDs themselves.
data "aws_subnet" "verify_producer" {
  provider = aws.producer
  for_each = { for i in range(3) : tostring(i) => i }
  id       = local.producer_subnet_ids[each.value]

  lifecycle {
    postcondition {
      condition = (
        self.vpc_id == module.producer_vpc.vpc_id && self.cidr_block == local.producer_subnet_cidrs[each.value] &&
        self.availability_zone == data.aws_availability_zones.producer.names[each.value] &&
        self.owner_id == data.aws_caller_identity.producer.account_id && !self.map_public_ip_on_launch
      )
      error_message = "Every producer subnet must belong to its warehouse VPC/account and occupy the requested CIDR and AZ without automatic public IPs."
    }
  }
}

data "aws_subnet" "verify_consumer" {
  provider = aws.consumer
  for_each = { for i in range(3) : tostring(i) => i }
  id       = local.consumer_subnet_ids[each.value]

  lifecycle {
    postcondition {
      condition = (
        self.vpc_id == module.consumer_vpc.vpc_id && self.cidr_block == local.consumer_subnet_cidrs[each.value] &&
        self.availability_zone == data.aws_availability_zones.consumer.names[each.value] &&
        self.owner_id == data.aws_caller_identity.consumer.account_id && !self.map_public_ip_on_launch
      )
      error_message = "Every consumer subnet must belong to its warehouse VPC/account and occupy the requested CIDR and AZ without automatic public IPs."
    }
  }
}

# Select by subnet to verify the effective association, not merely a route table that should have been attached.
# Wait for endpoint routes and all route-table associations; no additional network resources depend on these reads.
# The AWS route-table data source omits local and gateway-endpoint routes. Verify S3 attachment through the endpoint
# readback, and require every visible custom route to be the explicitly enabled internet default.
data "aws_route_table" "verify_producer" {
  provider  = aws.producer
  for_each  = { for i in range(3) : tostring(i) => i }
  subnet_id = local.producer_subnet_ids[each.value]

  depends_on = [module.producer_vpc, module.producer_endpoints]

  lifecycle {
    postcondition {
      condition = (
        self.vpc_id == module.producer_vpc.vpc_id &&
        contains(data.aws_vpc_endpoint.verify_producer["s3"].route_table_ids, self.id) &&
        anytrue([for association in self.associations : association.subnet_id == local.producer_subnet_ids[each.value] && association.route_table_id == self.id]) &&
        alltrue([for route in self.routes :
          (var.allow_public_sql && route.cidr_block == "0.0.0.0/0" && route.gateway_id == module.producer_vpc.igw_id)
        ]) &&
        (var.allow_public_sql ? (
          anytrue([for route in self.routes : route.cidr_block == "0.0.0.0/0" && route.gateway_id == module.producer_vpc.igw_id])
          ) : (
          alltrue([for route in self.routes : route.cidr_block != "0.0.0.0/0" && route.ipv6_cidr_block != "::/0"])
        ))
      )
      error_message = "The effective producer subnet route table must attach to the S3 endpoint and contain no visible custom route except the explicitly enabled internet default."
    }
  }
}

data "aws_route_table" "verify_consumer" {
  provider  = aws.consumer
  for_each  = { for i in range(3) : tostring(i) => i }
  subnet_id = local.consumer_subnet_ids[each.value]

  depends_on = [module.consumer_vpc, module.consumer_endpoints]

  lifecycle {
    postcondition {
      condition = (
        self.vpc_id == module.consumer_vpc.vpc_id &&
        contains(data.aws_vpc_endpoint.verify_consumer["s3"].route_table_ids, self.id) &&
        anytrue([for association in self.associations : association.subnet_id == local.consumer_subnet_ids[each.value] && association.route_table_id == self.id]) &&
        alltrue([for route in self.routes :
          (var.allow_public_sql && route.cidr_block == "0.0.0.0/0" && route.gateway_id == module.consumer_vpc.igw_id)
        ]) &&
        (var.allow_public_sql ? (
          anytrue([for route in self.routes : route.cidr_block == "0.0.0.0/0" && route.gateway_id == module.consumer_vpc.igw_id])
          ) : (
          alltrue([for route in self.routes : route.cidr_block != "0.0.0.0/0" && route.ipv6_cidr_block != "::/0"])
        ))
      )
      error_message = "The effective consumer subnet route table must attach to the S3 endpoint and contain no visible custom route except the explicitly enabled internet default."
    }
  }
}

data "aws_vpc_endpoint" "verify_producer" {
  provider = aws.producer
  for_each = toset(["s3", "glue"])
  id       = module.producer_endpoints.endpoints[each.key].id

  lifecycle {
    postcondition {
      condition = (
        self.state == "available" && self.vpc_id == module.producer_vpc.vpc_id &&
        self.service_name == "com.amazonaws.${var.region}.${each.key}" &&
        (each.key == "s3" ? (
          self.vpc_endpoint_type == "Gateway" &&
          toset(self.route_table_ids) == toset(concat(module.producer_vpc.intra_route_table_ids, module.producer_vpc.public_route_table_ids))
          ) : (
          self.vpc_endpoint_type == "Interface" && self.private_dns_enabled &&
          toset(self.subnet_ids) == toset(local.producer_subnet_ids) &&
          toset(self.security_group_ids) == toset([module.producer_endpoints.security_group_id])
        ))
      )
      error_message = "Producer endpoints must be available, serve the intended service, and attach to the actual warehouse network."
    }
  }
}

data "aws_vpc_endpoint" "verify_consumer" {
  provider = aws.consumer
  for_each = toset(concat(["s3", "glue"], local.sso_enabled ? ["sso_oauth", "identitystore"] : []))
  id       = module.consumer_endpoints.endpoints[each.key].id

  lifecycle {
    postcondition {
      condition = (
        self.state == "available" && self.vpc_id == module.consumer_vpc.vpc_id &&
        self.service_name == "com.amazonaws.${var.region}.${replace(each.key, "_", "-")}" &&
        (each.key == "s3" ? (
          self.vpc_endpoint_type == "Gateway" &&
          toset(self.route_table_ids) == toset(concat(module.consumer_vpc.intra_route_table_ids, module.consumer_vpc.public_route_table_ids))
          ) : (
          self.vpc_endpoint_type == "Interface" && self.private_dns_enabled &&
          toset(self.subnet_ids) == toset(local.consumer_subnet_ids) &&
          toset(self.security_group_ids) == toset([module.consumer_endpoints.security_group_id])
        ))
      )
      error_message = "Consumer endpoints, including optional SSO endpoints, must be available and attached to the warehouse network."
    }
  }
}

# List the entire group, including unmanaged rules: private mode must not silently tolerate extra ingress.
# Individual SQL lookups below verify the allowed rules; a listing alone cannot prove their ports or CIDRs.
data "aws_vpc_security_group_rules" "verify_producer" {
  provider = aws.producer
  filter {
    name   = "group-id"
    values = [aws_security_group.producer.id]
  }
  depends_on = [aws_vpc_security_group_ingress_rule.producer_sql, aws_vpc_security_group_egress_rule.producer_vpc, aws_vpc_security_group_egress_rule.producer_s3]

  lifecycle {
    postcondition {
      condition     = length(self.ids) == 2 + (var.allow_public_sql ? length(toset(var.public_sql_cidrs)) : 0)
      error_message = "The producer security group must contain only its two egress rules and explicitly allowed public SQL rules."
    }
  }
}

data "aws_vpc_security_group_rules" "verify_consumer" {
  provider = aws.consumer
  filter {
    name   = "group-id"
    values = [aws_security_group.consumer.id]
  }
  depends_on = [aws_vpc_security_group_ingress_rule.consumer_sql, aws_vpc_security_group_egress_rule.consumer_vpc, aws_vpc_security_group_egress_rule.consumer_s3]

  lifecycle {
    postcondition {
      condition     = length(self.ids) == 2 + (var.allow_public_sql ? length(toset(var.public_sql_cidrs)) : 0)
      error_message = "The consumer security group must contain only its two egress rules and explicitly allowed public SQL rules."
    }
  }
}

data "aws_vpc_security_group_rule" "verify_producer_sql" {
  provider               = aws.producer
  for_each               = aws_vpc_security_group_ingress_rule.producer_sql
  security_group_rule_id = each.value.security_group_rule_id

  lifecycle {
    postcondition {
      condition     = !self.is_egress && self.security_group_id == aws_security_group.producer.id && self.ip_protocol == "tcp" && self.from_port == 5439 && self.to_port == 5439 && self.cidr_ipv4 == each.key
      error_message = "Observed producer SQL ingress must allow only TCP/5439 from its explicitly requested IPv4 CIDR."
    }
  }
}

data "aws_vpc_security_group_rule" "verify_consumer_sql" {
  provider               = aws.consumer
  for_each               = aws_vpc_security_group_ingress_rule.consumer_sql
  security_group_rule_id = each.value.security_group_rule_id

  lifecycle {
    postcondition {
      condition     = !self.is_egress && self.security_group_id == aws_security_group.consumer.id && self.ip_protocol == "tcp" && self.from_port == 5439 && self.to_port == 5439 && self.cidr_ipv4 == each.key
      error_message = "Observed consumer SQL ingress must allow only TCP/5439 from its explicitly requested IPv4 CIDR."
    }
  }
}

data "aws_vpc_security_group_rule" "verify_producer_egress" {
  provider = aws.producer
  for_each = {
    vpc = aws_vpc_security_group_egress_rule.producer_vpc.security_group_rule_id
    s3  = aws_vpc_security_group_egress_rule.producer_s3.security_group_rule_id
  }
  security_group_rule_id = each.value

  lifecycle {
    postcondition {
      condition = self.is_egress && self.security_group_id == aws_security_group.producer.id && (each.key == "vpc" ? (
        self.ip_protocol == "-1" && self.cidr_ipv4 == var.producer_vpc_cidr
        ) : (
        self.ip_protocol == "tcp" && self.from_port == 443 && self.to_port == 443 && self.prefix_list_id == module.producer_endpoints.endpoints["s3"].prefix_list_id
      ))
      error_message = "Observed producer egress must be limited to its VPC and HTTPS to the S3 prefix list."
    }
  }
}

data "aws_vpc_security_group_rule" "verify_consumer_egress" {
  provider = aws.consumer
  for_each = {
    vpc = aws_vpc_security_group_egress_rule.consumer_vpc.security_group_rule_id
    s3  = aws_vpc_security_group_egress_rule.consumer_s3.security_group_rule_id
  }
  security_group_rule_id = each.value

  lifecycle {
    postcondition {
      condition = self.is_egress && self.security_group_id == aws_security_group.consumer.id && (each.key == "vpc" ? (
        self.ip_protocol == "-1" && self.cidr_ipv4 == var.consumer_vpc_cidr
        ) : (
        self.ip_protocol == "tcp" && self.from_port == 443 && self.to_port == 443 && self.prefix_list_id == module.consumer_endpoints.endpoints["s3"].prefix_list_id
      ))
      error_message = "Observed consumer egress must be limited to its VPC and HTTPS to the S3 prefix list."
    }
  }
}

data "aws_redshift_cluster" "verify_producer" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier

  lifecycle {
    postcondition {
      condition = (
        self.encrypted && self.enhanced_vpc_routing && self.publicly_accessible == var.allow_public_sql &&
        self.vpc_id == module.producer_vpc.vpc_id && self.node_type == var.cluster_node_type &&
        self.database_name == var.admin_database && self.master_username == var.admin_username && self.port == 5439 &&
        self.cluster_subnet_group_name == aws_redshift_subnet_group.producer.name &&
        self.cluster_parameter_group_name == aws_redshift_parameter_group.producer.name &&
        toset(self.vpc_security_group_ids) == toset([aws_security_group.producer.id]) &&
        toset(self.iam_roles) == toset([aws_iam_role.producer.arn]) && self.default_iam_role_arn == aws_iam_role.producer.arn &&
        self.cluster_namespace_arn == aws_redshift_cluster.producer.cluster_namespace_arn
      )
      error_message = "Redshift must report the encrypted producer's database, topology, parameter group, security group, and associated default IAM role."
    }
  }
}

data "aws_redshift_subnet_group" "verify_producer" {
  provider = aws.producer
  name     = aws_redshift_subnet_group.producer.name

  lifecycle {
    postcondition {
      condition     = toset(self.subnet_ids) == toset(local.producer_subnet_ids)
      error_message = "The observed producer subnet group must contain all three warehouse subnets and no others."
    }
  }
}

data "aws_redshiftserverless_namespace" "verify_consumer" {
  provider       = aws.consumer
  namespace_name = aws_redshiftserverless_namespace.consumer.namespace_name

  lifecycle {
    postcondition {
      condition = (
        self.namespace_id == aws_redshiftserverless_namespace.consumer.namespace_id &&
        self.arn == aws_redshiftserverless_namespace.consumer.arn &&
        try(split(":", self.arn)[4] == data.aws_caller_identity.consumer.account_id, false) &&
        self.db_name == var.admin_database && self.admin_username == var.admin_username &&
        self.default_iam_role_arn == aws_iam_role.consumer.arn && toset(self.iam_roles) == toset([aws_iam_role.consumer.arn])
      )
      error_message = "The consumer namespace must report the administrator database and intended associated default IAM role."
    }
  }
}

data "aws_redshiftserverless_workgroup" "verify_consumer" {
  provider       = aws.consumer
  workgroup_name = aws_redshiftserverless_workgroup.consumer.workgroup_name

  lifecycle {
    postcondition {
      condition = (
        self.namespace_name == aws_redshiftserverless_namespace.consumer.namespace_name &&
        self.enhanced_vpc_routing && self.publicly_accessible == var.allow_public_sql &&
        toset(self.subnet_ids) == toset(local.consumer_subnet_ids) &&
        toset(self.security_group_ids) == toset([aws_security_group.consumer.id]) &&
        try(self.endpoint[0].port == 5439 && self.endpoint[0].address == aws_redshiftserverless_workgroup.consumer.endpoint[0].address, false)
      )
      error_message = "The observed workgroup must bind the consumer namespace to its intended subnets/security group and SQL endpoint."
    }
  }
}

data "aws_iam_role" "verify_producer" {
  provider = aws.producer
  name     = aws_iam_role.producer.name

  lifecycle {
    postcondition {
      condition = try(
        self.arn == aws_iam_role.producer.arn &&
        split(":", self.arn)[4] == data.aws_caller_identity.producer.account_id &&
        length(jsondecode(self.assume_role_policy).Statement) == 1 &&
        jsondecode(self.assume_role_policy).Statement[0].Effect == "Allow" &&
        toset(keys(jsondecode(self.assume_role_policy).Statement[0].Principal)) == toset(["Service"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Principal.Service])) == toset(["redshift.amazonaws.com"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Action])) == toset(["sts:AssumeRole"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"]])) == toset([data.aws_caller_identity.producer.account_id]),
        false,
      )
      error_message = "The observed producer role trust must restrict assumption to Redshift in the producer account."
    }
  }
}

data "aws_iam_role" "verify_consumer" {
  provider = aws.consumer
  name     = aws_iam_role.consumer.name

  lifecycle {
    postcondition {
      condition = try(
        self.arn == aws_iam_role.consumer.arn &&
        split(":", self.arn)[4] == data.aws_caller_identity.consumer.account_id &&
        length(jsondecode(self.assume_role_policy).Statement) == 1 &&
        jsondecode(self.assume_role_policy).Statement[0].Effect == "Allow" &&
        toset(keys(jsondecode(self.assume_role_policy).Statement[0].Principal)) == toset(["Service"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Principal.Service])) == toset(["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Action])) == toset(["sts:AssumeRole", "sts:SetContext"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"]])) == toset([data.aws_caller_identity.consumer.account_id]),
        false,
      )
      error_message = "The observed consumer trust must allow only Redshift services in the consumer account, including context propagation."
    }
  }
}

data "aws_iam_role" "verify_lambda" {
  provider = aws.consumer
  name     = aws_iam_role.extfunctions_lambda.name

  lifecycle {
    postcondition {
      condition = try(
        self.arn == aws_iam_role.extfunctions_lambda.arn &&
        split(":", self.arn)[4] == data.aws_caller_identity.consumer.account_id &&
        length(jsondecode(self.assume_role_policy).Statement) == 1 &&
        jsondecode(self.assume_role_policy).Statement[0].Effect == "Allow" &&
        toset(keys(jsondecode(self.assume_role_policy).Statement[0].Principal)) == toset(["Service"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Principal.Service])) == toset(["lambda.amazonaws.com"]) &&
        toset(flatten([jsondecode(self.assume_role_policy).Statement[0].Action])) == toset(["sts:AssumeRole"]),
        false,
      )
      error_message = "The observed UDF execution role must trust Lambda, not a warehouse service or arbitrary principal."
    }
  }
}

# The API exposes inline policy names here, not policy documents. This independently checks attachment inventory,
# not the statements' effective permissions. The example's service/SQL operations exercise only the actions they use,
# not every granted action (in particular a Lambda definition readback does not invoke its code).
data "aws_iam_role_policies" "verify_producer" {
  provider  = aws.producer
  role_name = aws_iam_role.producer.name

  depends_on = [aws_iam_role_policy.producer_fixture, aws_iam_role_policy.producer_spectrum_tables]

  lifecycle {
    postcondition {
      condition     = self.policy_names == toset(["fixture", "spectrum-tables"])
      error_message = "The producer role must have exactly the fixture and Spectrum table inline policies attached."
    }
  }
}

data "aws_iam_role_policies" "verify_consumer" {
  provider  = aws.consumer
  role_name = aws_iam_role.consumer.name

  depends_on = [aws_iam_role_policy.consumer_fixture, aws_iam_role_policy.consumer_lambda_udf, aws_iam_role_policy.identity_center, aws_iam_role_policy.consumer_bedrock]

  lifecycle {
    postcondition {
      condition     = self.policy_names == toset(concat(["fixture", "lambda-udf"], local.sso_enabled ? ["identity-center"] : [], local.bedrock_enabled ? ["bedrock-model"] : []))
      error_message = "The consumer role must have exactly its fixture/UDF policies and the enabled Identity Center and Bedrock policies attached."
    }
  }
}

data "aws_iam_role_policy_attachments" "verify_lambda" {
  provider  = aws.consumer
  role_name = aws_iam_role.extfunctions_lambda.name

  depends_on = [aws_iam_role_policy_attachment.extfunctions_lambda_logs]

  lifecycle {
    postcondition {
      condition     = toset([for policy in self.attached_policies : policy.policy_arn]) == toset(["arn:${data.aws_partition.consumer.partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"])
      error_message = "The Lambda execution role must have only the basic execution managed policy attached."
    }
  }
}

data "aws_s3_bucket" "verify_fixture" {
  provider = aws.producer
  bucket   = module.fixture_bucket.s3_bucket_id

  lifecycle {
    postcondition {
      condition     = self.bucket_region == var.region && self.arn == module.fixture_bucket.s3_bucket_arn
      error_message = "The Spectrum/COPY fixture bucket must exist in the warehouse Region under the expected ARN."
    }
  }
}

data "aws_s3_bucket_policy" "verify_fixture" {
  provider = aws.producer
  bucket   = module.fixture_bucket.s3_bucket_id

  depends_on = [module.fixture_bucket]

  lifecycle {
    postcondition {
      condition = anytrue([for statement in try(jsondecode(self.policy).Statement, []) : try(
        statement.Effect == "Deny" && statement.Principal == "*" &&
        toset(flatten([statement.Action])) == toset(["s3:*"]) &&
        toset(flatten([statement.Resource])) == toset([module.fixture_bucket.s3_bucket_arn, "${module.fixture_bucket.s3_bucket_arn}/*"]) &&
        tostring(statement.Condition.Bool["aws:SecureTransport"]) == "false",
        false,
      )])
      error_message = "The actual fixture bucket policy must deny non-TLS access to both bucket and objects."
    }

    postcondition {
      condition = !local.cross_account || alltrue([for sid in ["ConsumerBucketMetadata", "ConsumerFixtureRead", "ConsumerUnload"] : anytrue([
        for statement in try(jsondecode(self.policy).Statement, []) : try(
          statement.Sid == sid && statement.Effect == "Allow" && toset(flatten([statement.Principal.AWS])) == toset([aws_iam_role.consumer.arn]) &&
          toset(flatten([statement.Action])) == toset(sid == "ConsumerBucketMetadata" ? ["s3:GetBucketLocation", "s3:ListBucket"] : (sid == "ConsumerFixtureRead" ? ["s3:GetObject"] : ["s3:PutObject", "s3:AbortMultipartUpload"])) &&
          toset(flatten([statement.Resource])) == toset(sid == "ConsumerBucketMetadata" ? [module.fixture_bucket.s3_bucket_arn] : (sid == "ConsumerFixtureRead" ? ["${module.fixture_bucket.s3_bucket_arn}/data/*", "${module.fixture_bucket.s3_bucket_arn}/unload/*"] : ["${module.fixture_bucket.s3_bucket_arn}/unload/*"])),
          false,
        )
      ])])
      error_message = "The actual cross-account bucket policy must grant the consumer role only the intended fixture/unload actions and prefixes."
    }

    postcondition {
      condition = !local.copy_job_enabled || anytrue([for statement in try(jsondecode(self.policy).Statement, []) : try(
        statement.Effect == "Allow" && toset(flatten([statement.Principal.Service])) == toset(["redshift.amazonaws.com"]) &&
        toset(flatten([statement.Action])) == toset(["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]) &&
        toset(flatten([statement.Resource])) == toset([module.fixture_bucket.s3_bucket_arn]) &&
        statement.Condition.StringEquals["aws:SourceAccount"] == data.aws_caller_identity.producer.account_id &&
        statement.Condition.ArnLike["aws:SourceArn"] == "arn:${data.aws_partition.consumer.partition}:redshift:${var.region}:${data.aws_caller_identity.producer.account_id}:integration:*",
        false,
      )])
      error_message = "The actual COPY JOB bucket policy must authorize Redshift notification management only for producer-account integrations in this Region."
    }
  }
}

data "aws_s3_object" "verify_fixture" {
  provider      = aws.producer
  bucket        = aws_s3_object.fixture.bucket
  key           = aws_s3_object.fixture.key
  download_body = true

  depends_on = [aws_s3_object.fixture]

  lifecycle {
    postcondition {
      condition     = self.body == "id,label\n1,first\n2,second\n" && self.content_type == "text/csv" && self.server_side_encryption == "AES256"
      error_message = "The downloaded fixture must contain the expected CSV rows and report SSE-S3 encryption and CSV content type."
    }
  }
}

data "aws_glue_catalog_table" "verify_fixture" {
  provider      = aws.producer
  catalog_id    = data.aws_caller_identity.producer.account_id
  database_name = aws_glue_catalog_table.fixture.database_name
  name          = aws_glue_catalog_table.fixture.name

  depends_on = [aws_glue_catalog_table.fixture]

  lifecycle {
    postcondition {
      condition = try(
        self.table_type == "EXTERNAL_TABLE" && self.parameters["skip.header.line.count"] == "1" &&
        self.storage_descriptor[0].location == "s3://${module.fixture_bucket.s3_bucket_id}/data/" &&
        self.storage_descriptor[0].input_format == "org.apache.hadoop.mapred.TextInputFormat" &&
        self.storage_descriptor[0].output_format == "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat" &&
        self.storage_descriptor[0].ser_de_info[0].serialization_library == "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe" &&
        self.storage_descriptor[0].ser_de_info[0].parameters["field.delim"] == "," &&
        [for column in self.storage_descriptor[0].columns : "${column.name}:${lower(column.type)}"] == ["id:int", "label:varchar(64)"],
        false,
      )
      error_message = "Glue must report the Spectrum fixture's CSV location, formats, delimiter, header handling, and ordered columns."
    }
  }
}

data "aws_lambda_function" "verify_upper" {
  provider      = aws.consumer
  function_name = aws_lambda_function.extfunctions_upper.function_name

  lifecycle {
    postcondition {
      condition = (
        self.role == aws_iam_role.extfunctions_lambda.arn && self.runtime == "python3.12" && self.handler == "index.handler" &&
        self.code_sha256 == data.archive_file.extfunctions_upper.output_base64sha256
      )
      error_message = "The deployed UDF Lambda must use the intended execution role, runtime, handler, and exact built archive."
    }
  }
}

# The module already persists this password; the readback verifies the current version rather than duplicating admin
# secrets. It does not print values in the failure message. SQL verify_shared subsequently exercises this credential.
data "aws_secretsmanager_secret_version" "verify_reader" {
  provider  = aws.consumer
  secret_id = module.reader_secret.secret_arn

  depends_on = [module.reader_secret]

  lifecycle {
    postcondition {
      condition = try(
        contains(self.version_stages, "AWSCURRENT") &&
        jsondecode(self.secret_string).username == redshift_user.reader.name &&
        jsondecode(self.secret_string).password == random_password.reader.result,
        false,
      )
      error_message = "The current reader secret must contain the managed SQL reader's username and password."
    }
  }
}

# Existing DynamoDB sources may use another key/billing mode; PITR is still required. Their resource policies and KMS
# key suitability remain operator prerequisites (the table lookup alone cannot distinguish the AWS-managed key).
data "aws_dynamodb_table" "verify_zero_etl" {
  provider = aws.consumer
  count    = local.zero_etl_enabled && local.zero_etl_dynamodb ? 1 : 0
  name     = split("/", local.zero_etl_source_arn)[1]

  depends_on = [aws_dynamodb_table.zero_etl_source]

  lifecycle {
    postcondition {
      condition = try(
        self.arn == local.zero_etl_source_arn && self.point_in_time_recovery[0].enabled &&
        (!var.zero_etl_create_dynamodb_source || (
          self.billing_mode == "PAY_PER_REQUEST" && self.hash_key == "order_id" &&
          anytrue([for attribute in self.attribute : attribute.name == "order_id" && attribute.type == "S"])
        )),
        false,
      )
      error_message = "The DynamoDB integration source must have PITR enabled; an owned source must also report the intended on-demand order key."
    }
  }
}

data "aws_rds_cluster" "verify_zero_etl" {
  provider           = aws.consumer
  count              = var.zero_etl_create_source ? 1 : 0
  cluster_identifier = aws_rds_cluster.zero_etl_source[0].cluster_identifier

  depends_on = [aws_rds_cluster_instance.zero_etl_source]

  lifecycle {
    postcondition {
      condition = (
        self.engine == "aurora-postgresql" && self.storage_encrypted && self.database_name == "orders" &&
        self.db_cluster_parameter_group_name == aws_rds_cluster_parameter_group.zero_etl_source[0].name &&
        self.db_subnet_group_name == aws_db_subnet_group.zero_etl_source[0].name &&
        toset(self.vpc_security_group_ids) == toset([aws_security_group.consumer.id]) &&
        contains(self.cluster_members, aws_rds_cluster_instance.zero_etl_source[0].identifier)
      )
      error_message = "The owned Aurora source must report its encrypted database, replication parameter group, network bindings, and running writer member."
    }
  }
}

data "aws_db_instance" "verify_zero_etl" {
  provider               = aws.consumer
  count                  = var.zero_etl_create_source ? 1 : 0
  db_instance_identifier = aws_rds_cluster_instance.zero_etl_source[0].identifier

  lifecycle {
    postcondition {
      condition = (
        self.db_cluster_identifier == aws_rds_cluster.zero_etl_source[0].cluster_identifier &&
        self.engine == "aurora-postgresql" && self.db_instance_class == "db.serverless" &&
        self.storage_encrypted && !self.publicly_accessible
      )
      error_message = "The owned Aurora writer must belong to the source cluster and remain encrypted, serverless, and private even in public SQL mode."
    }
  }
}

# Redshift's IDC API has no data source, but the resulting managed SSO application and its assignments are observable.
data "aws_ssoadmin_application" "verify_consumer" {
  provider        = aws.consumer
  count           = local.sso_enabled ? 1 : 0
  application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn

  lifecycle {
    postcondition {
      condition     = self.instance_arn == var.identity_center_instance_arn && self.status == "ENABLED"
      error_message = "The managed Redshift SSO application must be enabled in the selected Identity Center instance."
    }
  }
}

data "aws_identitystore_group" "verify_consumer" {
  provider = aws.consumer
  for_each = local.sso_enabled ? {
    readers   = local.sso_reader_group
    operators = local.sso_operator_group
  } : {}
  identity_store_id = local.identity_store_id
  group_id          = each.value.id

  lifecycle {
    postcondition {
      condition     = self.display_name == each.value.name
      error_message = "The assigned Identity Center group ID must resolve to the display name mapped into Redshift roles."
    }
  }
}

data "aws_ssoadmin_application_assignments" "verify_consumer" {
  provider        = aws.consumer
  count           = local.sso_enabled ? 1 : 0
  application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn

  depends_on = [aws_ssoadmin_application_assignment.readers, aws_ssoadmin_application_assignment.operators]

  lifecycle {
    postcondition {
      condition = toset([for assignment in self.application_assignments : "${assignment.principal_type}:${assignment.principal_id}"]) == toset([
        "GROUP:${local.sso_reader_group.id}", "GROUP:${local.sso_operator_group.id}",
      ])
      error_message = "The managed SSO application must have exactly the reader and operator group assignments."
    }
  }
}
