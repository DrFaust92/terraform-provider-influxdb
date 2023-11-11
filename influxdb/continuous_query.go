package influxdb

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/influxdata/influxdb/client"
)

func resourceContinuousQuery() *schema.Resource {
	return &schema.Resource{
		Create: createContinuousQuery,
		Read:   readContinuousQuery,
		Delete: deleteContinuousQuery,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"database": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"query": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"resample": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Default:  "",
			},
		},
	}
}

func createContinuousQuery(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)

	name := d.Get("name").(string)
	database := d.Get("database").(string)
	resample := d.Get("resample").(string)
	quer := d.Get("query").(string)

	var queryStr string
	if resample == "" {
		queryStr = fmt.Sprintf("CREATE CONTINUOUS QUERY %q ON %q BEGIN %s END", name, database, quer)
	} else {
		queryStr = fmt.Sprintf("CREATE CONTINUOUS QUERY %q ON %q RESAMPLE %s BEGIN %s END", name, database, resample, quer)
	}

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

	d.SetId(fmt.Sprintf("%s:%s", name, database))

	err = readContinuousQuery(d, meta)
	if err != nil {
		return err
	}
	// check that cq is created
	if d.Id() == "" {
		return fmt.Errorf("Unable to create continuous query '%s', check your sql query.", name)
	}

	return nil
}

func readContinuousQuery(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name, database, err := continuousQueryId(d.Id())
	if err != nil {
		return err
	}

	// InfluxDB doesn't have a command to check the existence of a single
	// ContinuousQuery, so we instead must read the list of all ContinuousQuerys and see
	// if ours is present in it.
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
		if series.Name == database {
			for _, result := range series.Values {
				if result[0].(string) == name {
					d.Set("name", name)
					d.Set("database", database)

					return nil
				}
			}
		}
	}

	// If we fell out here then we didn't find our ContinuousQuery in the list.
	d.SetId("")

	return nil
}

func deleteContinuousQuery(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*client.Client)
	name := d.Get("name").(string)
	database := d.Get("database").(string)

	queryStr := fmt.Sprintf("DROP CONTINUOUS QUERY %q ON %q", name, database)
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

func continuousQueryId(id string) (string, string, error) {
	parts := strings.SplitN(id, ":", 2)

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected format of ID (%s), expected NAME:DB-ID", id)
	}

	return parts[0], parts[1], nil
}
