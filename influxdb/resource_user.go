package influxdb

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/influxdata/influxdb/client"
)

func resourceUser() *schema.Resource {
	return &schema.Resource{
		Create: createUser,
		Read:   readUser,
		Update: updateUser,
		Delete: deleteUser,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"password": {
				Type:      schema.TypeString,
				Required:  true,
				ForceNew:  true,
				Sensitive: true,
				StateFunc: hashSum,
			},
			"admin": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
			},
			"grant": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"database": {
							Type:     schema.TypeString,
							Required: true,
						},
						"privilege": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice([]string{"READ", "WRITE", "ALL"}, false),
						},
					},
				},
			},
		},
	}
}

func createUser(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)

	name := d.Get("name").(string)
	password := d.Get("password").(string)

	is_admin := d.Get("admin").(bool)
	admin_privileges := ""
	if is_admin {
		admin_privileges = "WITH ALL PRIVILEGES"
	}

	queryStr := fmt.Sprintf("CREATE USER %q WITH PASSWORD '%s' %s", name, password, admin_privileges)
	query := client.Query{
		Command: queryStr,
	}

	resp, err := conn.Query(query)
	if err != nil {
		return err
	}
	if resp.Err != nil {
		return resp.Err
	}

	d.SetId(name)

	if v, ok := d.GetOk("grant"); ok {
		grants := v.(*schema.Set).List()
		for _, vv := range grants {
			grant := vv.(map[string]interface{})
			if err := grantPrivilegeOn(conn, grant["privilege"].(string), grant["database"].(string), name); err != nil {
				return err
			}
		}
	}

	return readUser(d, meta)
}

func grantPrivilegeOn(conn *client.Client, privilege, database, user string) error {
	return exec(conn, fmt.Sprintf("GRANT %s ON %q TO %q", privilege, database, user))
}

func revokePrivilegeOn(conn *client.Client, privilege, database, user string) error {
	return exec(conn, fmt.Sprintf("REVOKE %s ON %q FROM %q", privilege, database, user))
}

func grantAllOn(conn *client.Client, user string) error {
	return exec(conn, fmt.Sprintf("GRANT ALL PRIVILEGES TO %q", user))
}

func revokeAllOn(conn *client.Client, user string) error {
	return exec(conn, fmt.Sprintf("REVOKE ALL PRIVILEGES FROM %q", user))
}

func readUser(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name := d.Id()

	// InfluxDB doesn't have a command to check the existence of a single
	// User, so we instead must read the list of all Users and see
	// if ours is present in it.
	query := client.Query{
		Command: "SHOW USERS",
	}

	resp, err := conn.Query(query)
	if err != nil {
		return err
	}
	if resp.Err != nil {
		return resp.Err
	}

	var found = false
	for _, result := range resp.Results[0].Series[0].Values {
		if result[0] == name {
			found = true
			d.Set("name", name)
			d.Set("admin", result[1].(bool))
			break
		}
	}

	if !found {
		// If we fell out here then we didn't find our User in the list.
		d.SetId("")

		return nil
	}

	return readGrants(d, meta)
}

func readGrants(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name := d.Id()

	query := client.Query{
		Command: fmt.Sprintf("SHOW GRANTS FOR %q", name),
	}

	resp, err := conn.Query(query)
	if err != nil {
		return err
	}

	if resp.Err != nil {
		return resp.Err
	}

	var grants = []map[string]string{}
	for _, result := range resp.Results[0].Series[0].Values {
		if result[1].(string) != "NO PRIVILEGES" {
			var grant = map[string]string{
				"database":  result[0].(string),
				"privilege": strings.Replace(strings.ToUpper(result[1].(string)), "ALL PRIVILEGES", "ALL", 1),
			}
			grants = append(grants, grant)
		}
	}
	d.Set("grant", grants)
	return nil
}

func updateUser(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name := d.Id()

	if d.HasChange("admin") {
		if !d.Get("admin").(bool) {
			err := revokeAllOn(conn, name)
			if err != nil {
				return err
			}
		} else {
			err := grantAllOn(conn, name)
			if err != nil {
				return err
			}
		}
	}

	if d.HasChange("grant") {
		oldGrantV, newGrantV := d.GetChange("grant")
		oldGrant := oldGrantV.(*schema.Set).List()
		newGrant := newGrantV.(*schema.Set).List()

		for _, oGV := range oldGrant {
			oldGrant := oGV.(map[string]interface{})

			exists := false
			privilege := oldGrant["privilege"].(string)
			for _, nGV := range newGrant {
				newGrant := nGV.(map[string]interface{})

				if newGrant["database"].(string) == oldGrant["database"].(string) {
					exists = true
					privilege = newGrant["privilege"].(string)
				}
			}

			if !exists {
				err := revokePrivilegeOn(conn, oldGrant["privilege"].(string), oldGrant["database"].(string), name)
				if err != nil {
					return err
				}
			} else {
				if privilege != oldGrant["privilege"].(string) {
					err := grantPrivilegeOn(conn, privilege, oldGrant["database"].(string), name)
					if err != nil {
						return err
					}
				}
			}
		}

		for _, nGV := range newGrant {
			newGrant := nGV.(map[string]interface{})

			exists := false
			for _, oGV := range oldGrant {
				oldGrant := oGV.(map[string]interface{})

				exists = exists || (newGrant["database"].(string) == oldGrant["database"].(string) && newGrant["privilege"].(string) == oldGrant["privilege"].(string))
			}

			if !exists {
				err := grantPrivilegeOn(conn, newGrant["privilege"].(string), newGrant["database"].(string), name)
				if err != nil {
					return err
				}
			}
		}
	}

	return readUser(d, meta)
}

func deleteUser(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name := d.Id()

	queryStr := fmt.Sprintf("DROP USER %q", name)
	query := client.Query{
		Command: queryStr,
	}

	resp, err := conn.Query(query)
	if err != nil {
		return err
	}
	if resp.Err != nil {
		return resp.Err
	}

	return nil
}
