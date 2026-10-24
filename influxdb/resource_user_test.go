package influxdb

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/influxdata/influxdb/client"
)

func TestAccInfluxDBUser_admin(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resourceName := "influxdb_user.test"
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccUserConfig_admin(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "true"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "0"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				Config: testAccUserConfig_revoke(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserExists(resourceName),
					testAccCheckUserNoAdmin(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "false"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "0"),
				),
			},
		},
	})
}

func TestAccInfluxDBUser_grant(t *testing.T) {
	resourceName := "influxdb_user.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccUserConfig_grant(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "false"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "grant.*", map[string]string{
						"database":  rName,
						"privilege": "READ",
					}),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				Config: testAccUserConfig_grantUpdate(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "false"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "grant.*", map[string]string{
						"database":  rName,
						"privilege": "ALL",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "grant.*", map[string]string{
						"database":  fmt.Sprintf("%s-2", rName),
						"privilege": "WRITE",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "grant.*", map[string]string{
						"database":  fmt.Sprintf("%s-3", rName),
						"privilege": "READ",
					}),
				),
			},
		},
	})
}

func TestAccInfluxDBUser_revoke(t *testing.T) {
	resourceName := "influxdb_user.test"
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccUserConfig_grant(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "false"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "grant.*", map[string]string{
						"database":  rName,
						"privilege": "READ",
					}),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				Config: testAccUserConfig_revoke(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttrSet(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "admin", "false"),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "0"),
				),
			},
		},
	})
}

func testAccCheckUserExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No user id set")
		}

		conn := testAccProvider.Meta().(*client.Client)

		query := client.Query{
			Command: "SHOW USERS",
		}

		resp, err := conn.Query(query)
		if err != nil {
			return err
		}

		if resp.Error() != nil {
			return resp.Error()
		}

		for _, result := range resp.Results[0].Series[0].Values {
			if result[0] == rs.Primary.ID {
				return nil
			}
		}

		return fmt.Errorf("User %q does not exist", rs.Primary.ID)
	}
}

func testAccCheckUserNoAdmin(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No user id set")
		}

		conn := testAccProvider.Meta().(*client.Client)

		query := client.Query{
			Command: "SHOW USERS",
		}

		resp, err := conn.Query(query)
		if err != nil {
			return err
		}

		if resp.Error() != nil {
			return resp.Error()
		}

		for _, result := range resp.Results[0].Series[0].Values {
			if result[0] == rs.Primary.ID {
				if result[1].(bool) == true {
					return fmt.Errorf("User %q is admin", rs.Primary.ID)
				}

				return nil
			}
		}

		return fmt.Errorf("User %q does not exist", rs.Primary.ID)
	}
}

func testAccUserConfig_admin(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_user" "test" {
  name     = %[1]q
  password = %[1]q
  admin    = true
}
`, rName)
}

func testAccUserConfig_grant(rName string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
}

resource "influxdb_user" "test" {
  name     = %[1]q
  password = %[1]q
  
  grant {
    database = influxdb_database.test.name
    privilege = "READ"
  }
}
`, rName)
}

func testAccUserConfig_revoke(rName string) string {
	return fmt.Sprintf(`	
resource "influxdb_database" "test" {
  name = %[1]q
}

resource "influxdb_user" "test" {
  name     = influxdb_database.test.name
  password = %[1]q
  admin    = false
}
`, rName)
}

func testAccUserConfig_grantUpdate(rName string) string {
	return fmt.Sprintf(`		
resource "influxdb_database" "test" {
    name = %[1]q
}

resource "influxdb_database" "test2" {
    name = "%[1]s-2"
}

resource "influxdb_database" "test3" {
    name = "%[1]s-3"
}

resource "influxdb_user" "test" {
  name     = %[1]q
  password = %[1]q

  grant {
    database = influxdb_database.test.name
    privilege = "ALL"
  }
  
  grant {
    database = influxdb_database.test2.name
    privilege = "WRITE"
  }
  
  grant {
    database = influxdb_database.test3.name
    privilege = "READ"
  }
}
`, rName)
}

func TestAccInfluxDBUser_writeOnlyPassword(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	resourceName := "influxdb_user.test"
	// A quote checks the password is escaped in the InfluxQL string literal.
	password := "it's-" + acctest.RandString(10)
	rotated := "rotated-" + acctest.RandString(10)

	resource.Test(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"influxdb": func() (*schema.Provider, error) { return Provider(), nil },
		},
		Steps: []resource.TestStep{
			{
				Config: testAccUserConfig_writeOnly(rName, password, "1"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserLogin(rName, password, true),
					testAccCheckNotInState(password),
					resource.TestCheckNoResourceAttr(resourceName, "password"),
					resource.TestCheckResourceAttr(resourceName, "password_wo_version", "1"),
				),
			},
			{
				// Bumping the version rotates the password in place.
				Config: testAccUserConfig_writeOnly(rName, rotated, "2"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserLogin(rName, rotated, true),
					testAccCheckUserLogin(rName, password, false),
					testAccCheckNotInState(rotated),
				),
			},
		},
	})
}

func testAccUserConfig_writeOnly(rName, password, version string) string {
	return fmt.Sprintf(`
resource "influxdb_user" "test" {
  name                = %[1]q
  password_wo         = %[2]q
  password_wo_version = %[3]q
}
`, rName, password, version)
}

// testAccCheckUserLogin connects to InfluxDB as the user and checks whether the password works.
func testAccCheckUserLogin(user, password string, wantOK bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		u, err := url.Parse(os.Getenv("INFLUXDB_URL"))
		if err != nil || u.Host == "" {
			u, _ = url.Parse("http://localhost:8086/")
		}

		conn, err := client.NewClient(client.Config{URL: *u, Username: user, Password: password})
		if err != nil {
			return err
		}

		resp, err := conn.Query(client.Query{Command: "SHOW DATABASES"})
		if err == nil && resp.Error() != nil {
			err = resp.Error()
		}

		if wantOK && err != nil {
			return fmt.Errorf("login as %q failed: %w", user, err)
		}
		if !wantOK && err == nil {
			return fmt.Errorf("login as %q with the old password unexpectedly succeeded", user)
		}
		return nil
	}
}

// testAccCheckNotInState fails if the secret, or its hash, appears in any attribute in state.
func testAccCheckNotInState(secret string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for name, rs := range s.RootModule().Resources {
			for k, v := range rs.Primary.Attributes {
				if strings.Contains(v, secret) || v == hashSum(secret) {
					return fmt.Errorf("secret found in state: %s.%s", name, k)
				}
			}
		}
		return nil
	}
}
