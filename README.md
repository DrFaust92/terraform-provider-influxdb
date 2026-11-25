# Terraform Provider for InfluxDB

Manages [InfluxDB](https://www.influxdata.com/) 1.x databases, retention policies, users and continuous queries.

- Registry: [DrFaust92/influxdb](https://registry.terraform.io/providers/DrFaust92/influxdb/latest)
- Documentation: [docs/](docs/), generated from the provider schema
- Release notes: [GitHub releases](https://github.com/DrFaust92/terraform-provider-influxdb/releases)

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) 1.0 or later (1.11 or later for write-only arguments)
- InfluxDB 1.x. Acceptance tests run against 1.11 and 1.12; InfluxDB 2.x and 3.x are not supported
- [Go](https://go.dev/doc/install), at the version in `go.mod`, to build the provider

## Usage

```terraform
terraform {
  required_providers {
    influxdb = {
      source  = "DrFaust92/influxdb"
      version = "~> 2.0"
    }
  }
}

provider "influxdb" {
  url = "http://localhost:8086/"
}

resource "influxdb_database" "metrics" {
  name = "metrics"
}
```

## Development

```sh
make build   # build and install the provider into $GOPATH/bin
make test    # unit tests
make docs    # regenerate docs/ from the schema, examples/ and templates/
```

Acceptance tests need a running InfluxDB 1.x with authentication enabled:

```sh
docker run -d -p 8086:8086 \
  -e INFLUXDB_HTTP_AUTH_ENABLED=true \
  -e INFLUXDB_ADMIN_USER=test -e INFLUXDB_ADMIN_PASSWORD=test \
  influxdb:1.12
make testacc
```

Set `INFLUXDB_URL`, `INFLUXDB_USERNAME` and `INFLUXDB_PASSWORD` to test against another server.
