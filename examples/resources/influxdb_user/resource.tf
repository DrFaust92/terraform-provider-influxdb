resource "influxdb_database" "green" {
  name = "terraform-green"
}

resource "influxdb_user" "paul" {
  name     = "paul"
  password = var.paul_password

  grant {
    database  = influxdb_database.green.name
    privilege = "WRITE"
  }
}

# Keep the password out of state (Terraform 1.11+). Bump
# password_wo_version to apply a new password_wo.
resource "influxdb_user" "ringo" {
  name                = "ringo"
  password_wo         = var.ringo_password
  password_wo_version = "1"
}
