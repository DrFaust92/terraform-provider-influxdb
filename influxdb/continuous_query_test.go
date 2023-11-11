package influxdb

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/influxdata/influxdb/client"
)

func TestAccInfluxDBContiuousQuery_basic(t *testing.T) {
	resourceName := "influxdb_continuous_query.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccContiuousQueryBasicConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckContiuousQueryExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "database", rName),
					resource.TestCheckResourceAttr(resourceName, "query", "SELECT min(mouse) INTO min_mouse FROM zoo GROUP BY time(30m)"),
				),
			},
		},
	})
}

func TestAccInfluxDBContiuousQuery_resample(t *testing.T) {
	resourceName := "influxdb_continuous_query.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccContiuousQueryResampleConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckContiuousQueryExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "database", rName),
					resource.TestCheckResourceAttr(resourceName, "query", "SELECT min(mouse) INTO min_mouse_resampled FROM zoo GROUP BY time(30m)"),
					resource.TestCheckResourceAttr(resourceName, "resample", "EVERY 30m FOR 90m"),
				),
			},
		},
	})
}

func TestAccContiuousQueryConfig(t *testing.T) {
	resourceName := "influxdb_continuous_query.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccContiuousQueryConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckContiuousQueryExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "database", rName),
					resource.TestCheckResourceAttr(resourceName, "query", "SELECT count(arrival_time) AS count INTO tooling_rp.:MEASUREMENT FROM raw_default_rp./.*/ WHERE tech_source_id = 'S2' GROUP BY time(1d), project, tech_source_id"),
				),
			},
		},
	})
}

func TestAccContiuousWithFailureQueryConfig(t *testing.T) {
	//resourceName := "influxdb_continuous_query.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccContiuousQueryWithFailureConfig(rName),
				// No check function is given because we expect this configuration
				// to fail before any infrastructure is created
				ExpectError: regexp.MustCompile("Unable to create continuous query"),
			},
		},
	})
}

func testAccCheckContiuousQueryExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ContiuousQuery id set")
		}

		conn := testAccProvider.Meta().(*client.Client)

		query := client.Query{
			Command: "SHOW CONTINUOUS QUERIES",
		}

		resp, err := conn.Query(query)
		if err != nil {
			return err
		}

		if resp.Err != nil {
			return resp.Err
		}

		for _, series := range resp.Results[0].Series {
			if series.Name == rs.Primary.Attributes["database"] {
				for _, result := range series.Values {
					if result[0].(string) == rs.Primary.Attributes["name"] {
						return nil
					}
				}
			}
		}

		return fmt.Errorf("ContiuousQuery %q does not exist", rs.Primary.ID)
	}
}

func testAccContiuousQueryBasicConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
}

resource "influxdb_continuous_query" "test" {
  name     = %[1]q
  database = influxdb_database.test.name
  query    = "SELECT min(mouse) INTO min_mouse FROM zoo GROUP BY time(30m)"
}
`, rName)
}

func testAccContiuousQueryConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q

  retention_policies {
	name = "tooling_rp"
	 duration = "24h0m0s"
	 default = "false"
   }
   retention_policies {
	name = "raw_default_rp"
	 duration = "24h0m0s"
	 default = "false"
   }

}

resource "influxdb_continuous_query" "test" {
  name     = %[1]q
  database = influxdb_database.test.name
  query    = "SELECT count(arrival_time) AS count INTO tooling_rp.:MEASUREMENT FROM raw_default_rp./.*/ WHERE tech_source_id = 'S2' GROUP BY time(1d), project, tech_source_id"
}
`, rName)
}

func testAccContiuousQueryWithFailureConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q

   retention_policies {
	name = "raw_default_rp"
	 duration = "24h0m0s"
	 default = "false"
   }

}

resource "influxdb_continuous_query" "test" {
  name     = %[1]q
  database = influxdb_database.test.name
  query    = "SELECT count(arrival_time) AS count INTO tooling_rp.:MEASUREMENT FROM raw_default_rp./.*/ WHERE tech_source_id = 'S2' GROUP BY time(1d), project, tech_source_id"
}
`, rName)
}

func testAccContiuousQueryResampleConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
}

resource "influxdb_continuous_query" "test" {
  name     = %[1]q
  database = influxdb_database.test.name
  query    = "SELECT min(mouse) INTO min_mouse_resampled FROM zoo GROUP BY time(30m)"
  resample = "EVERY 30m FOR 90m"
}
`, rName)
}
