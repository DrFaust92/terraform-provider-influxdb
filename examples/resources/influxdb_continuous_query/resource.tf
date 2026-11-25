resource "influxdb_database" "test" {
  name = "terraform-test"
}

resource "influxdb_continuous_query" "minnie" {
  name     = "minnie"
  database = influxdb_database.test.name
  query    = "SELECT min(mouse) INTO min_mouse FROM zoo GROUP BY time(30m)"
}

resource "influxdb_continuous_query" "minnie_resample" {
  name     = "minnie_resample"
  database = influxdb_database.test.name
  query    = "SELECT min(mouse) INTO min_mouse_resample FROM zoo GROUP BY time(30m)"
  resample = "EVERY 30m FOR 2h"
}
