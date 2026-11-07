# Mocked runs for users, groups, and group memberships. The mocks are shared with composition.tftest.hcl through tests/mocks, so
# this block adds runs and assertions here without editing the composition suite.
mock_provider "aws" {
  alias  = "producer"
  source = "./tests/mocks/aws_producer"
}

mock_provider "aws" {
  alias  = "consumer"
  source = "./tests/mocks/aws_consumer"
}

mock_provider "random" {
  source = "./tests/mocks/random"
}

mock_provider "redshift" { alias = "producer" }
mock_provider "redshift" {
  alias  = "consumer"
  source = "./tests/mocks/redshift_consumer"
}
mock_provider "redshift" { alias = "producer_data_api_iam" }
mock_provider "redshift" { alias = "consumer_data_api_iam" }
mock_provider "redshift" { alias = "producer_direct_iam" }
mock_provider "redshift" { alias = "consumer_direct_iam" }
mock_provider "redshift" { alias = "producer_direct_password" }
mock_provider "redshift" { alias = "consumer_direct_password" }

# Observed lookup values differ from the configuration where the lookup, not the resource, must be the source.
override_data {
  target = data.redshift_user.reader
  values = {
    search_path      = ["$user", "public"]
    session_defaults = { timezone = "UTC", statement_timeout = "300000" }
    connection_limit = 5
    session_timeout  = 3600
  }
}
override_data {
  target = data.redshift_group.readers
  values = { group_id = 104, members = ["example_reader"] }
}
override_data {
  target = data.redshift_users.all
  values = { id = "users-listing-identity" }
}
override_data {
  target = data.redshift_groups.all
  values = { id = "groups-listing-identity" }
}
override_data {
  target = data.redshift_group_memberships.readers
  values = { id = "reader-group-memberships-listing-identity" }
}

run "users_apply" {
  command = apply

  assert {
    condition = (
      redshift_user.reader.valid_until == "infinity" && redshift_user.reader.connection_limit == 5 &&
      redshift_user.reader.session_timeout == 3600 && redshift_user.reader.syslog_access == "RESTRICTED" &&
      redshift_user.reader.search_path == tolist(["$user", "public"]) &&
      redshift_user.reader.session_defaults == tomap({ timezone = "UTC", statement_timeout = "300000" })
    )
    error_message = "The reader must configure its sign-in limits and stored session defaults."
  }

  assert {
    condition = (
      redshift_user.iam_auditor.password_disabled && redshift_user.iam_auditor.syslog_access == "UNRESTRICTED" &&
      redshift_user.iam_auditor.connection_limit == 2
    )
    error_message = "The IAM-only auditor must have its password disabled and unrestricted system log access."
  }

  assert {
    condition = (
      data.redshift_group.readers.name == redshift_group_membership.reader.group &&
      output.user_settings == {
        reader_search_path      = tolist(["$user", "public"])
        reader_session_defaults = tomap({ timezone = "UTC", statement_timeout = "300000" })
        reader_connection_limit = 5
        reader_session_timeout  = 3600
        reader_group_id         = 104
        reader_group_members    = toset(["example_reader"])
      }
    )
    error_message = "User and group lookups must expose the reader's options and the reader group's members."
  }

  assert {
    condition = (
      data.redshift_users.all.id == "users-listing-identity" && data.redshift_groups.all.id == "groups-listing-identity" &&
      data.redshift_group_memberships.readers.group == redshift_group_membership.reader.group &&
      data.redshift_group_memberships.readers.user == null &&
      output.user_listings.users == [for user in data.redshift_users.all.users : user.name] &&
      output.user_listings.groups == { for group in data.redshift_groups.all.groups : group.name => group.members } &&
      output.user_listings.reader_group_members == [
        for membership in data.redshift_group_memberships.readers.group_memberships : membership.user
      ]
    )
    error_message = "User, group, and membership listings must reach user_listings, with the membership listing narrowed to the reader group."
  }
}
