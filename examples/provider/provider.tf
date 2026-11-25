terraform {
  required_providers {
    influxdb = {
      source  = "DrFaust92/influxdb"
      version = "~> 2.0"
    }
  }
}

# The URL and credentials can also come from the INFLUXDB_URL,
# INFLUXDB_USERNAME and INFLUXDB_PASSWORD environment variables.
provider "influxdb" {
  url      = "https://influxdb.example.com:8086/"
  username = "terraform"
  password = var.influxdb_password
}
