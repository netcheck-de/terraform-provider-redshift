output "user_settings" {
  description = "Observed reader sign-in options and stored session defaults, and the reader group's ID and members."
  value = {
    reader_search_path      = data.redshift_user.reader.search_path
    reader_session_defaults = data.redshift_user.reader.session_defaults
    reader_connection_limit = data.redshift_user.reader.connection_limit
    reader_session_timeout  = data.redshift_user.reader.session_timeout
    reader_group_id         = data.redshift_group.readers.group_id
    reader_group_members    = data.redshift_group.readers.members
  }
}

output "user_listings" {
  description = "Warehouse-wide user and group listings, and the reader group's members as listed memberships."
  value = {
    users                = [for user in data.redshift_users.all.users : user.name]
    reader_connections   = one([for user in data.redshift_users.all.users : user.connection_limit if user.name == redshift_user.reader.name])
    groups               = { for group in data.redshift_groups.all.groups : group.name => group.members }
    reader_group_members = [for membership in data.redshift_group_memberships.readers.group_memberships : membership.user]
  }
}
