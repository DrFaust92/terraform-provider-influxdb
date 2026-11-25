resource "influxdb_database" "metrics" {
  name = "awesome_app"
}

resource "influxdb_database" "example" {
  name = "example"

  retention_policies {
    name     = "2days"
    duration = "48h0m0s"
  }

  retention_policies {
    name     = "12weeks"
    duration = "2016h0m0s"
    default  = true
  }

  retention_policies {
    name               = "1week"
    duration           = "168h0m0s"
    shardgroupduration = "2h0m0s"
  }
}
