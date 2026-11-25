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

# A server with a private CA that requires client certificates. Requests
# also go through the proxy in HTTPS_PROXY, unless NO_PROXY excludes the host.
provider "influxdb" {
  alias              = "tls"
  url                = "https://influxdb.internal:8086/"
  ca_certificate     = file("${path.module}/ca.pem")
  client_certificate = file("${path.module}/client.pem")
  client_key         = file("${path.module}/client-key.pem")
  timeout            = "5m"
}
