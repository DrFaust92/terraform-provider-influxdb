package influxdb

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/influxdata/influxdb/client"
)

func TestAccInfluxDBDatabase_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resourceName := "influxdb_database.test"
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccDatabaseConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDatabaseExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "retention_policies.#", "0"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccInfluxDBDatabase_retention(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resourceName := "influxdb_database.test"
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccDatabaseWithRPSConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDatabaseExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "retention_policies.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "retention_policies.*", map[string]string{
						"name":     "1day",
						"duration": "24h0m0s",
						"default":  "false",
					}),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccDatabaseWithRPSUpdateConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDatabaseExists(resourceName),
					resource.TestCheckResourceAttr("influxdb_database.test", "name", rName),
					resource.TestCheckResourceAttr(resourceName, "retention_policies.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "retention_policies.*", map[string]string{
						"name":     "2days",
						"duration": "48h0m0s",
						"default":  "false",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "retention_policies.*", map[string]string{
						"name":     "12weeks",
						"duration": "2016h0m0s",
						"default":  "false",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "retention_policies.*", map[string]string{
						"name":               "1week",
						"duration":           "168h0m0s",
						"default":            "false",
						"shardgroupduration": "2h0m0s",
					}),
				),
			},
		},
	})
}

func testAccCheckDatabaseExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No database id set")
		}

		conn := testAccProvider.Meta().(*client.Client)

		query := client.Query{
			Command: "SHOW DATABASES",
		}

		resp, err := conn.Query(query)
		if err != nil {
			return err
		}

		if resp.Error() != nil {
			return resp.Error()
		}

		for _, result := range resp.Results[0].Series[0].Values {
			if result[0] == rs.Primary.Attributes["name"] {
				return nil
			}
		}

		return fmt.Errorf("Database %q does not exist", rs.Primary.Attributes["name"])
	}
}

func testAccDatabaseConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
}
`, rName)
}

func testAccDatabaseWithRPSConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
  retention_policies {
   name = "1day"
    duration = "24h0m0s"
    default = "false"
  }
}
`, rName)
}

func testAccDatabaseWithRPSUpdateConfig(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
  
  retention_policies {
    name     = "2days"
    duration = "48h0m0s"
  }
  
  retention_policies {
    name     = "12weeks"
    duration = "2016h0m0s"
    default  = "false"
  }
  
  retention_policies {
    name               = "1week"
    duration           = "168h0m0s"
    shardgroupduration = "2h0m0s"
  }
  }
  `, rName)
}

// Managing the "autogen" policy that InfluxDB creates with every database, both when it is added to an
// existing database and when the database is created with it (#11).
func TestAccInfluxDBDatabase_defaultRetentionPolicy(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	rNameCreate := acctest.RandomWithPrefix("tf-acc-test")
	resourceName := "influxdb_database.test"

	resource.Test(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"influxdb": func() (*schema.Provider, error) { return Provider(), nil },
		},
		Steps: []resource.TestStep{
			{
				Config: testAccDatabaseConfig(rName),
				Check:  resource.TestCheckResourceAttr(resourceName, "retention_policies.#", "0"),
			},
			{
				Config: testAccDatabaseWithAutogenConfig(rName, "336h0m0s"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRetentionPolicyDuration(rName, "autogen", "336h0m0s"),
					resource.TestCheckResourceAttr(resourceName, "retention_policies.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "retention_policies.*", map[string]string{
						"name":     "autogen",
						"duration": "336h0m0s",
						"default":  "true",
					}),
				),
			},
			{
				// InfluxDB rejects this, the error used to be ignored and left a perpetual diff.
				Config:      testAccDatabaseWithAutogenConfig(rName, "24h0m0s"),
				ExpectError: regexp.MustCompile("retention policy duration must be greater than the shard duration"),
			},
			{
				Config: testAccDatabaseWithAutogenConfig(rNameCreate, "336h0m0s"),
				Check:  testAccCheckRetentionPolicyDuration(rNameCreate, "autogen", "336h0m0s"),
			},
		},
	})
}

func testAccDatabaseWithAutogenConfig(rName, duration string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q

  retention_policies {
    name               = "autogen"
    duration           = %[2]q
    shardgroupduration = "168h0m0s"
    default            = true
  }
}
`, rName, duration)
}

// testAccCheckRetentionPolicyDuration checks the retention policy duration on the server.
func testAccCheckRetentionPolicyDuration(database, policy, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		u, err := url.Parse(os.Getenv("INFLUXDB_URL"))
		if err != nil || u.Host == "" {
			u, _ = url.Parse("http://localhost:8086/")
		}

		conn, err := client.NewClient(client.Config{URL: *u, Username: os.Getenv("INFLUXDB_USERNAME"), Password: os.Getenv("INFLUXDB_PASSWORD")})
		if err != nil {
			return err
		}

		resp, err := conn.Query(client.Query{Command: fmt.Sprintf("SHOW RETENTION POLICIES ON %q", database)})
		if err != nil {
			return err
		}
		if resp.Error() != nil {
			return resp.Error()
		}

		for _, row := range firstSeriesValues(resp) {
			if row[0] == policy {
				if row[1] != want {
					return fmt.Errorf("retention policy %q duration = %v, want %s", policy, row[1], want)
				}
				return nil
			}
		}

		return fmt.Errorf("retention policy %q not found on %q", policy, database)
	}
}
