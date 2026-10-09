mock_resource "random_id" {
  defaults = { hex = "deadbeef" }
}
mock_resource "random_password" {
  defaults = { result = "MockOnlyPassword123456789" }
}
