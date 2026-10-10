data "redshift_datashares" "all" {}

data "redshift_datashares" "outbound" {
  share_type = "OUTBOUND"
}
