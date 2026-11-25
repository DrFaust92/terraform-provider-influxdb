package influxdb

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/influxdata/influxdb/client"
)

// To run these acceptance tests, you will need an InfluxDB server.
// If you download an InfluxDB distribution and run it with its default
// settings, on the same host where the tests are being run, then these tests
// should work with no further configuration.
//
// To run the tests against a remote InfluxDB server, set the INFLUXDB_URL,
// INFLUXDB_USERNAME and INFLUXDB_PASSWORD environment variables.

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"influxdb": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccClient returns an InfluxDB client configured like the provider, from
// the environment.
func testAccClient() (*client.Client, error) {
	u, err := url.Parse(os.Getenv("INFLUXDB_URL"))
	if err != nil || u.Host == "" {
		u, _ = url.Parse(defaultURL)
	}

	return client.NewClient(client.Config{
		URL:      *u,
		Username: os.Getenv("INFLUXDB_USERNAME"),
		Password: os.Getenv("INFLUXDB_PASSWORD"),
	})
}

func TestProvider(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	resp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema: %v", diags)
	}
}
