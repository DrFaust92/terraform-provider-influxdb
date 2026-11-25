package influxdb

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Invalid provider arguments fail at plan time, before connecting.
func TestAccInfluxDBProvider_validation(t *testing.T) {
	database := `
resource "influxdb_database" "v" {
  name = "validation"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `provider "influxdb" { client_key = "key" }` + database,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`client_certificate`),
			},
			{
				Config:      `provider "influxdb" { ca_certificate = "/etc/ssl/ca.pem" }` + database,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid PEM certificate`),
			},
			{
				Config:      `provider "influxdb" { timeout = "0s" }` + database,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid duration`),
			},
		},
	})
}
