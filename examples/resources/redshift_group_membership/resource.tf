resource "redshift_group_membership" "reader" {
  group = redshift_group.readers.name
  user  = redshift_user.reader.name
}
