package influxdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

// httpTimeout bounds each API request so an unresponsive server cannot hang a
// plan or apply indefinitely.
const httpTimeout = 2 * time.Minute

const defaultURL = "http://localhost:8086/"

var _ provider.Provider = &influxdbProvider{}

type influxdbProvider struct {
	version string
}

type providerModel struct {
	URL           types.String `tfsdk:"url"`
	Username      types.String `tfsdk:"username"`
	Password      types.String `tfsdk:"password"`
	SkipSSLVerify types.Bool   `tfsdk:"skip_ssl_verify"`
}

// New returns a constructor for the provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &influxdbProvider{version: version}
	}
}

func (p *influxdbProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "influxdb"
	resp.Version = p.version
}

func (p *influxdbProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages InfluxDB 1.x databases, users and continuous queries.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "InfluxDB connection URL. Can also be set with the `INFLUXDB_URL` environment variable. Defaults to `" + defaultURL + "`.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "InfluxDB user name. Can also be set with the `INFLUXDB_USERNAME` environment variable.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "InfluxDB password. Can also be set with the `INFLUXDB_PASSWORD` environment variable.",
			},
			"skip_ssl_verify": schema.BoolAttribute{
				Optional:    true,
				Description: "Skip TLS certificate verification. Can also be set with the `INFLUXDB_SKIP_SSL_VERIFY` environment variable.",
			},
		},
	}
}

func (p *influxdbProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.URL.IsUnknown() || config.Username.IsUnknown() || config.Password.IsUnknown() || config.SkipSSLVerify.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration", "The InfluxDB provider configuration depends on values that are not known yet.")
		return
	}

	rawURL := stringOrEnv(config.URL, "INFLUXDB_URL", defaultURL)
	u, err := url.Parse(rawURL)
	if err != nil {
		resp.Diagnostics.AddError("Invalid InfluxDB URL", err.Error())
		return
	}

	skipSSLVerify := false
	if !config.SkipSSLVerify.IsNull() {
		skipSSLVerify = config.SkipSSLVerify.ValueBool()
	} else if v := os.Getenv("INFLUXDB_SKIP_SSL_VERIFY"); v != "" {
		skipSSLVerify, err = strconv.ParseBool(v)
		if err != nil {
			resp.Diagnostics.AddError("Invalid INFLUXDB_SKIP_SSL_VERIFY", err.Error())
			return
		}
	}

	conn, err := client.NewClient(client.Config{
		URL:       *u,
		Username:  stringOrEnv(config.Username, "INFLUXDB_USERNAME", ""),
		Password:  stringOrEnv(config.Password, "INFLUXDB_PASSWORD", ""),
		UnsafeSsl: skipSSLVerify,
		Timeout:   httpTimeout,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create InfluxDB client", err.Error())
		return
	}

	// The server must already exist when the provider is used; dependencies
	// between modules have to be managed by the configuration.
	if _, _, err := conn.Ping(); err != nil {
		resp.Diagnostics.AddError("Unable to reach InfluxDB", fmt.Sprintf("error pinging server: %s", err))
		return
	}

	resp.ResourceData = conn
}

func (p *influxdbProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newDatabaseResource,
		newUserResource,
		newContinuousQueryResource,
	}
}

func (p *influxdbProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// stringOrEnv returns the configured value, else the environment variable, else def.
func stringOrEnv(v types.String, env, def string) string {
	if !v.IsNull() {
		return v.ValueString()
	}
	if e, ok := os.LookupEnv(env); ok {
		return e
	}
	return def
}

// clientFromProviderData extracts the InfluxDB client passed to resources by Configure.
func clientFromProviderData(data any) (*client.Client, error) {
	if data == nil {
		return nil, nil
	}
	conn, ok := data.(*client.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected provider data type %T", data)
	}
	return conn, nil
}

func exec(conn *client.Client, query string) error {
	resp, err := conn.Query(client.Query{
		Command: query,
	})
	if err != nil {
		return err
	}
	if resp.Error() != nil {
		return resp.Error()
	}
	return nil
}

// queryResponse runs a query and returns its response, or the query or response error.
func queryResponse(conn *client.Client, query string) (*client.Response, error) {
	resp, err := conn.Query(client.Query{Command: query})
	if err != nil {
		return nil, err
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}
	return resp, nil
}
