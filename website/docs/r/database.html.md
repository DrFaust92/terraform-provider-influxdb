---
layout: "influxdb"
page_title: "InfluxDB: influxdb_database"
sidebar_current: "docs-influxdb-resource-database"
description: |-
  The influxdb_database resource allows an InfluxDB database to be created.
---

# influxdb\_database

The database resource allows a database to be created on an InfluxDB server.

## Example Usage

```hcl
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
    default  = "true"
  }
 
  retention_policies {
    name               = "1week"
    duration           = "168h0m0s"
    shardgroupduration = "2h0m0s"
  }
}
```

## Argument Reference

The following arguments are supported:

* `name` - (Required) The name for the database. This must be unique on the
  InfluxDB server.
* `retention_policies` - (Optional) A list of retention policies for specified database

Each `retention_policies` supports the following:

* `name` - (Required) The name of the retention policy.
* `duration` - (Required) The duration for retention policy, format of duration can be found at InfluxDB Documentation. Duration has to be passed as `0h0m0s`.
* `replication` - (Optional) Determines how many copies of data points are stored in a cluster. Not applicable for single node / Open Source version of InfluxDB. Default value of `1`.
* `shardgroupduration` - (Optional) Determines how much time each shard group spans. How and why to modify can be found at InfluxDB Documentation. Defaults to `1h0m0s`.
* `default` - (Optional) Marks current retention policy as default. Default value is `false`.

InfluxDB creates an `autogen` retention policy with every database. It is not tracked while it has
its default settings and is not in the configuration. To manage it, add a `retention_policies` block
named `autogen`: the existing policy is then altered instead of created. Removing that block later
drops the policy.

## Attributes Reference

* `id` - The name for the database.

## Import

Databases can be imported using the `name`.

```sh
terraform import influxdb_database.example example
```
