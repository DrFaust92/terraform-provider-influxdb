---
layout: "influxdb"
page_title: "InfluxDB: influxdb_user"
sidebar_current: "docs-influxdb-resource-user"
description: |-
  The influxdb_user resource allows an InfluxDB users to be managed.
---

# influxdb\_user

The user resource allows a user to be created on an InfluxDB server.

## Example Usage

```hcl
resource "influxdb_database" "green" {
    name = "terraform-green"
}

resource "influxdb_user" "paul" {
    name = "paul"
    password = "super-secret"

    grant {
      database = "${influxdb_database.green.name}"
      privilege = "write"
    }
}
```

## Argument Reference

The following arguments are supported:

* `name` - (Required) The name for the user.
* `password` - (Optional) The password for the user. Exactly one of `password` or `password_wo` must be set. Changing it recreates the user. A SHA-256 hash of it is stored in state.
* `password_wo` - (Optional) Write-only alternative to `password`, never stored in Terraform state or plan output. Requires Terraform 1.11 or later. Conflicts with `password`. See [write-only arguments](https://developer.hashicorp.com/terraform/language/manage-sensitive-data/write-only).
* `password_wo_version` - (Optional) Required with `password_wo`. Terraform cannot detect changes to write-only values, change this value (e.g. `"2"`) to update the password in place with `SET PASSWORD`.
* `admin` - (Optional) Mark the user as admin.
* `grant` - (Optional) A list of grants for non-admin users

Each `grant` supports the following:

* `database` - (Required) The name of the database the privilege is associated with
* `privilege` - (Required) The privilege to grant (READ|WRITE|ALL)

## Attributes Reference

* `id` - The name for the user.

## Import

Users can be imported using the `name`.

```sh
terraform import influxdb_user.example example
```
