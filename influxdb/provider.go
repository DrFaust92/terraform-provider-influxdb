package influxdb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

// defaultTimeout bounds each API request so an unresponsive server cannot
// hang a plan or apply indefinitely.
const defaultTimeout = 2 * time.Minute

const defaultURL = "http://localhost:8086/"

var _ provider.Provider = &influxdbProvider{}

type influxdbProvider struct {
	version string
}

type providerModel struct {
	URL               types.String `tfsdk:"url"`
	Username          types.String `tfsdk:"username"`
	Password          types.String `tfsdk:"password"`
	SkipSSLVerify     types.Bool   `tfsdk:"skip_ssl_verify"`
	CACertificate     types.String `tfsdk:"ca_certificate"`
	ClientCertificate types.String `tfsdk:"client_certificate"`
	ClientKey         types.String `tfsdk:"client_key"`
	Timeout           types.String `tfsdk:"timeout"`
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
				Description: "InfluxDB connection URL. Can also be set with the `INFLUXDB_URL` environment variable. Defaults to `" + defaultURL + "`. Requests go through the proxy set in the `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` environment variables.",
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
				Description: "Skip TLS certificate verification. Can also be set with the `INFLUXDB_SKIP_SSL_VERIFY` environment variable. Prefer `ca_certificate` for servers with a private CA.",
			},
			"ca_certificate": schema.StringAttribute{
				Optional:    true,
				Description: "PEM-encoded CA certificates used to verify the server certificate, instead of the system roots. Can also be set with the `INFLUXDB_CA_CERTIFICATE` environment variable.",
				Validators:  []validator.String{pemCertificateValidator{}},
			},
			"client_certificate": schema.StringAttribute{
				Optional:    true,
				Description: "PEM-encoded client certificate for servers that require TLS client authentication. Requires `client_key`. Can also be set with the `INFLUXDB_CLIENT_CERTIFICATE` environment variable.",
				Validators:  []validator.String{pemCertificateValidator{}},
			},
			"client_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "PEM-encoded private key of `client_certificate`. Can also be set with the `INFLUXDB_CLIENT_KEY` environment variable.",
			},
			"timeout": schema.StringAttribute{
				Optional:    true,
				Description: "Timeout of each request to InfluxDB, as a duration such as `30s` or `5m`. Can also be set with the `INFLUXDB_TIMEOUT` environment variable. Defaults to `2m`.",
				Validators:  []validator.String{durationValidator{}},
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

	for _, v := range []interface{ IsUnknown() bool }{
		config.URL, config.Username, config.Password, config.SkipSSLVerify,
		config.CACertificate, config.ClientCertificate, config.ClientKey, config.Timeout,
	} {
		if v.IsUnknown() {
			resp.Diagnostics.AddError("Unknown provider configuration", "The InfluxDB provider configuration depends on values that are not known yet.")
			return
		}
	}

	clientConfig, err := newClientConfig(config)
	if err != nil {
		resp.Diagnostics.AddError("Invalid InfluxDB provider configuration", err.Error())
		return
	}

	conn, err := client.NewClient(clientConfig)
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

// newClientConfig builds the InfluxDB client configuration from the provider
// configuration, falling back to the INFLUXDB_* environment variables.
func newClientConfig(config providerModel) (client.Config, error) {
	u, err := url.Parse(stringOrEnv(config.URL, "INFLUXDB_URL", defaultURL))
	if err != nil {
		return client.Config{}, fmt.Errorf("url: %w", err)
	}

	skipSSLVerify := false
	if !config.SkipSSLVerify.IsNull() {
		skipSSLVerify = config.SkipSSLVerify.ValueBool()
	} else if v := os.Getenv("INFLUXDB_SKIP_SSL_VERIFY"); v != "" {
		skipSSLVerify, err = strconv.ParseBool(v)
		if err != nil {
			return client.Config{}, fmt.Errorf("INFLUXDB_SKIP_SSL_VERIFY: %w", err)
		}
	}

	timeout := defaultTimeout
	if v := stringOrEnv(config.Timeout, "INFLUXDB_TIMEOUT", ""); v != "" {
		timeout, err = time.ParseDuration(v)
		if err != nil {
			return client.Config{}, fmt.Errorf("timeout: %w", err)
		}
		if timeout <= 0 {
			return client.Config{}, errors.New("timeout: must be positive")
		}
	}

	tlsConfig, err := newTLSConfig(
		stringOrEnv(config.CACertificate, "INFLUXDB_CA_CERTIFICATE", ""),
		stringOrEnv(config.ClientCertificate, "INFLUXDB_CLIENT_CERTIFICATE", ""),
		stringOrEnv(config.ClientKey, "INFLUXDB_CLIENT_KEY", ""),
	)
	if err != nil {
		return client.Config{}, err
	}

	return client.Config{
		URL:       *u,
		Username:  stringOrEnv(config.Username, "INFLUXDB_USERNAME", ""),
		Password:  stringOrEnv(config.Password, "INFLUXDB_PASSWORD", ""),
		UnsafeSsl: skipSSLVerify,
		Timeout:   timeout,
		TLS:       tlsConfig,
		// The client uses no proxy unless one is set.
		Proxy: http.ProxyFromEnvironment,
	}, nil
}

// newTLSConfig returns the TLS configuration for the given PEM values, or nil
// when none are set.
func newTLSConfig(caCert, clientCert, clientKey string) (*tls.Config, error) {
	if caCert == "" && clientCert == "" && clientKey == "" {
		return nil, nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if caCert != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caCert)) {
			return nil, errors.New("ca_certificate: no PEM-encoded certificate found")
		}
		tlsConfig.RootCAs = pool
	}

	if (clientCert == "") != (clientKey == "") {
		return nil, errors.New("client_certificate and client_key must be set together")
	}
	if clientCert != "" {
		pair, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
		if err != nil {
			return nil, fmt.Errorf("client_certificate and client_key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}

	return tlsConfig, nil
}

type pemCertificateValidator struct{}

func (pemCertificateValidator) Description(context.Context) string {
	return "value must contain a PEM-encoded certificate"
}

func (v pemCertificateValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (pemCertificateValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !x509.NewCertPool().AppendCertsFromPEM([]byte(req.ConfigValue.ValueString())) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid PEM certificate", "The value does not contain a PEM-encoded certificate.")
	}
}

type durationValidator struct{}

func (durationValidator) Description(context.Context) string {
	return "value must be a positive duration such as 30s or 5m"
}

func (v durationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (durationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	d, err := time.ParseDuration(req.ConfigValue.ValueString())
	if err != nil || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid duration", "Expected a positive duration such as 30s or 5m.")
	}
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
