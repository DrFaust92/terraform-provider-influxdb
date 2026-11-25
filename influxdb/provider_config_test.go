package influxdb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	oexec "os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

// testCA is a throwaway certificate authority for TLS tests.
type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert: cert, key: key, certPEM: pemEncode("CERTIFICATE", der)}
}

// issue returns a PEM certificate and key signed by the CA.
func (ca *testCA) issue(t *testing.T, usage x509.ExtKeyUsage) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pemEncode("CERTIFICATE", der), pemEncode("EC PRIVATE KEY", keyDER)
}

func pemEncode(typ string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

// pingHandler answers /ping like InfluxDB.
func pingHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Influxdb-Version", "1.12.0")
	w.WriteHeader(http.StatusNoContent)
}

// newTLSServer starts an HTTPS server with a certificate from ca. When
// clientCA is set, the server requires a client certificate signed by it.
func newTLSServer(t *testing.T, ca, clientCA *testCA) *httptest.Server {
	t.Helper()
	certPEM, keyPEM := ca.issue(t, x509.ExtKeyUsageServerAuth)
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(pingHandler))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	if clientCA != nil {
		pool := x509.NewCertPool()
		pool.AddCert(clientCA.cert)
		srv.TLS.ClientCAs = pool
		srv.TLS.ClientAuth = tls.RequireAndVerifyClientCert
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// clearInfluxEnv keeps the INFLUXDB_* variables of the environment out of the
// test. Empty values count as unset for every setting the tests use.
func clearInfluxEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "INFLUXDB_") {
			t.Setenv(name, "")
		}
	}
}

func ping(t *testing.T, m providerModel) error {
	t.Helper()
	cfg, err := newClientConfig(m)
	if err != nil {
		t.Fatalf("newClientConfig: %v", err)
	}
	conn, err := client.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, _, err = conn.Ping()
	return err
}

func baseModel(url string) providerModel {
	return providerModel{
		URL:               types.StringValue(url),
		Username:          types.StringNull(),
		Password:          types.StringNull(),
		SkipSSLVerify:     types.BoolNull(),
		CACertificate:     types.StringNull(),
		ClientCertificate: types.StringNull(),
		ClientKey:         types.StringNull(),
		Timeout:           types.StringNull(),
	}
}

func TestClientConfig_caCertificate(t *testing.T) {
	clearInfluxEnv(t)
	ca := newTestCA(t)
	srv := newTLSServer(t, ca, nil)

	m := baseModel(srv.URL)
	if err := ping(t, m); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("without ca_certificate: expected a certificate error, got %v", err)
	}

	m.CACertificate = types.StringValue(ca.certPEM)
	if err := ping(t, m); err != nil {
		t.Fatalf("with ca_certificate: %v", err)
	}

	// The environment variable is used when the argument is not set.
	t.Setenv("INFLUXDB_CA_CERTIFICATE", ca.certPEM)
	if err := ping(t, baseModel(srv.URL)); err != nil {
		t.Fatalf("with INFLUXDB_CA_CERTIFICATE: %v", err)
	}
}

func TestClientConfig_clientCertificate(t *testing.T) {
	clearInfluxEnv(t)
	ca := newTestCA(t)
	clientCA := newTestCA(t)
	srv := newTLSServer(t, ca, clientCA)

	m := baseModel(srv.URL)
	m.CACertificate = types.StringValue(ca.certPEM)
	if err := ping(t, m); err == nil {
		t.Fatal("without a client certificate: expected the server to reject the connection")
	}

	certPEM, keyPEM := clientCA.issue(t, x509.ExtKeyUsageClientAuth)
	m.ClientCertificate = types.StringValue(certPEM)
	m.ClientKey = types.StringValue(keyPEM)
	if err := ping(t, m); err != nil {
		t.Fatalf("with a client certificate: %v", err)
	}

	// A certificate from a CA the server does not trust is rejected.
	otherCert, otherKey := newTestCA(t).issue(t, x509.ExtKeyUsageClientAuth)
	m.ClientCertificate = types.StringValue(otherCert)
	m.ClientKey = types.StringValue(otherKey)
	if err := ping(t, m); err == nil {
		t.Fatal("with an untrusted client certificate: expected the server to reject the connection")
	}
}

func TestClientConfig_timeout(t *testing.T) {
	clearInfluxEnv(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		pingHandler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	m := baseModel(srv.URL)
	m.Timeout = types.StringValue("100ms")
	start := time.Now()
	err := ping(t, m)
	if err == nil || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}

	cfg, err := newClientConfig(baseModel(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != defaultTimeout {
		t.Fatalf("default timeout = %s, want %s", cfg.Timeout, defaultTimeout)
	}
}

// TestClientConfig_proxy checks that requests go through HTTP_PROXY. Go reads
// the proxy environment once per process, so the check runs in a child
// process started with the proxy set.
func TestClientConfig_proxy(t *testing.T) {
	if os.Getenv("INFLUXDB_TEST_PROXY_CHILD") == "1" {
		// INFLUXDB_URL names a host that does not resolve: only the proxy can answer.
		if err := ping(t, baseModel(os.Getenv("INFLUXDB_URL"))); err != nil {
			t.Fatalf("ping through proxy: %v", err)
		}
		return
	}

	var (
		mu    sync.Mutex
		hosts []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		pingHandler(w, r)
	}))
	t.Cleanup(proxy.Close)

	cmd := oexec.Command(os.Args[0], "-test.run=^TestClientConfig_proxy$", "-test.v")
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REQUEST_METHOD":
			continue
		}
		if strings.HasPrefix(name, "INFLUXDB_") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env,
		"INFLUXDB_TEST_PROXY_CHILD=1",
		"HTTP_PROXY="+proxy.URL,
		"INFLUXDB_URL=http://influxdb.invalid:8086/",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process: %v\n%s", err, out)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hosts) == 0 || hosts[0] != "influxdb.invalid:8086" {
		t.Fatalf("proxy saw hosts %v, want influxdb.invalid:8086", hosts)
	}
}

func TestNewTLSConfig_errors(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM := ca.issue(t, x509.ExtKeyUsageClientAuth)
	_, otherKey := ca.issue(t, x509.ExtKeyUsageClientAuth)

	cases := map[string]struct {
		ca, cert, key string
		want          string
	}{
		"bad CA":           {ca: "not a certificate", want: "ca_certificate"},
		"cert without key": {cert: certPEM, want: "must be set together"},
		"key without cert": {key: keyPEM, want: "must be set together"},
		"mismatched key":   {cert: certPEM, key: otherKey, want: "client_certificate and client_key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newTLSConfig(tc.ca, tc.cert, tc.key)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}

	if cfg, err := newTLSConfig("", "", ""); cfg != nil || err != nil {
		t.Fatalf("no TLS settings: got %v, %v", cfg, err)
	}
}

func TestValidators(t *testing.T) {
	ca := newTestCA(t)
	cases := []struct {
		v       validator.String
		value   string
		wantErr bool
	}{
		{durationValidator{}, "30s", false},
		{durationValidator{}, "5m", false},
		{durationValidator{}, "0s", true},
		{durationValidator{}, "-1m", true},
		{durationValidator{}, "30", true},
		{pemCertificateValidator{}, ca.certPEM, false},
		{pemCertificateValidator{}, "/etc/ssl/ca.pem", true},
	}
	for _, tc := range cases {
		resp := &validator.StringResponse{}
		tc.v.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("x"),
			ConfigValue: types.StringValue(tc.value),
		}, resp)
		if got := resp.Diagnostics.HasError(); got != tc.wantErr {
			t.Errorf("%T(%q): error = %v, want %v", tc.v, tc.value, got, tc.wantErr)
		}
	}
}
