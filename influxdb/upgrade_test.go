package influxdb

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Upgrade tests apply a configuration with the last terraform-plugin-sdk
// release of the provider and check that this build plans no changes for it.

const lastSDKv2Version = "1.9.1"

var lastSDKv2Provider = map[string]resource.ExternalProvider{
	"influxdb": {
		Source:            "DrFaust92/influxdb",
		VersionConstraint: lastSDKv2Version,
	},
}

// testAccUpgradeSteps returns a step that applies config with the SDKv2
// release and a step that requires an empty plan from this build.
func testAccUpgradeSteps(config string, check resource.TestCheckFunc) []resource.TestStep {
	return []resource.TestStep{
		{
			ExternalProviders: lastSDKv2Provider,
			Config:            config,
			Check:             check,
		},
		{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Config:                   config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		},
	}
}

func TestAccInfluxDBDatabase_upgradeFromSDKv2(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Steps: testAccUpgradeSteps(testAccDatabaseWithRPSUpdateConfig(rName), testAccCheckDatabaseExists("influxdb_database.test")),
	})
}

func TestAccInfluxDBDatabase_upgradeFromSDKv2NoPolicies(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Steps: testAccUpgradeSteps(testAccDatabaseConfig(rName), testAccCheckDatabaseExists("influxdb_database.test")),
	})
}

func TestAccInfluxDBDatabase_upgradeFromSDKv2Autogen(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Steps: testAccUpgradeSteps(testAccDatabaseWithAutogenConfig(rName, "336h0m0s"), testAccCheckDatabaseExists("influxdb_database.test")),
	})
}

func TestAccInfluxDBContinuousQuery_upgradeFromSDKv2(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		Steps: testAccUpgradeSteps(testAccContiuousQueryResampleConfig(rName), testAccCheckContiuousQueryExists("influxdb_continuous_query.test")),
	})
}

func TestAccInfluxDBUser_upgradeFromSDKv2WriteOnly(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	password := "it's-" + acctest.RandString(10)

	resource.Test(t, resource.TestCase{
		Steps: testAccUpgradeSteps(testAccUserConfig_writeOnlyWithGrant(rName, password, "1"), testAccCheckUserLogin(rName, password, true)),
	})
}

// SDKv2 releases stored a hash of password in state. The first apply with
// this build updates the user in place (it only replaces the hash with the
// password in state, without changing the password), and plans nothing after.
func TestAccInfluxDBUser_upgradeFromSDKv2Password(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	resourceName := "influxdb_user.test"
	config := testAccUserConfig_grantUpdate(rName)

	resource.Test(t, resource.TestCase{
		Steps: []resource.TestStep{
			{
				ExternalProviders: lastSDKv2Provider,
				Config:            config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "password", hashSum(rName)),
				),
			},
			{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "password", rName),
					resource.TestCheckResourceAttr(resourceName, "grant.#", "3"),
					testAccCheckUserLogin(rName, rName, true),
				),
			},
			{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   config,
				PlanOnly:                 true,
			},
		},
	})
}

func testAccUserConfig_writeOnlyWithGrant(rName, password, version string) string {
	return fmt.Sprintf(`
resource "influxdb_database" "test" {
  name = %[1]q
}

resource "influxdb_user" "test" {
  name                = %[1]q
  password_wo         = %[2]q
  password_wo_version = %[3]q
  admin               = false

  grant {
    database  = influxdb_database.test.name
    privilege = "READ"
  }
}
`, rName, password, version)
}
