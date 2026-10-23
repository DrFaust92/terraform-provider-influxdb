package influxdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// fakeInflux is a minimal InfluxDB 1.x HTTP API that tracks users and records every query.
type fakeInflux struct {
	mu      sync.Mutex
	users   map[string]bool
	queries []string
}

var createUserRe = regexp.MustCompile(`^CREATE USER "([^"]+)"`)
var dropUserRe = regexp.MustCompile(`^DROP USER "([^"]+)"`)

func (f *fakeInflux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Influxdb-Version", "1.8.10")
	if r.URL.Path == "/ping" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_ = r.ParseForm()
	q := r.Form.Get("q")
	f.queries = append(f.queries, q)

	result := map[string]interface{}{"statement_id": 0}
	switch {
	case createUserRe.MatchString(q):
		f.users[createUserRe.FindStringSubmatch(q)[1]] = true
	case dropUserRe.MatchString(q):
		delete(f.users, dropUserRe.FindStringSubmatch(q)[1])
	case q == "SHOW USERS":
		values := [][]interface{}{}
		for u := range f.users {
			values = append(values, []interface{}{u, false})
		}
		result["series"] = []map[string]interface{}{{"columns": []string{"user", "admin"}, "values": values}}
	case strings.HasPrefix(q, "SHOW GRANTS"):
		result["series"] = []map[string]interface{}{{"columns": []string{"database", "privilege"}, "values": [][]interface{}{}}}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{result}})
}

func (f *fakeInflux) hasQuery(want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, q := range f.queries {
			if strings.TrimSpace(q) == want {
				return nil
			}
		}
		return fmt.Errorf("query %q not sent, got %q", want, f.queries)
	}
}

func testCheckSecretNotInState(secret string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for name, rs := range s.RootModule().Resources {
			for k, v := range rs.Primary.Attributes {
				if strings.Contains(v, secret) || v == hashSum(secret) {
					return fmt.Errorf("secret (or its hash) found in state: %s.%s", name, k)
				}
			}
		}
		return nil
	}
}

func TestWriteOnlyUserPassword(t *testing.T) {
	fake := &fakeInflux{users: map[string]bool{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	t.Setenv("INFLUXDB_URL", server.URL)

	config := func(password, version string) string {
		return fmt.Sprintf(`
resource "influxdb_user" "test" {
  name                = "alice"
  password_wo         = %q
  password_wo_version = %q
}
`, password, version)
	}

	resource.UnitTest(t, resource.TestCase{
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("it's-secret", "1"),
				Check: resource.ComposeTestCheckFunc(
					// The quote in the password must be escaped in the InfluxQL string literal.
					fake.hasQuery(`CREATE USER "alice" WITH PASSWORD 'it\'s-secret'`),
					testCheckSecretNotInState("it's-secret"),
				),
			},
			{
				// Bumping the version rotates the password in place instead of recreating the user.
				Config: config("rotated", "2"),
				Check: resource.ComposeTestCheckFunc(
					fake.hasQuery(`SET PASSWORD FOR "alice" = 'rotated'`),
					testCheckSecretNotInState("rotated"),
				),
			},
		},
	})
}

func TestQuoteString(t *testing.T) {
	cases := map[string]string{
		"plain":        `'plain'`,
		"it's":         `'it\'s'`,
		`back\slash`:   `'back\\slash'`,
		`x' OR '1'='1`: `'x\' OR \'1\'=\'1'`,
		"new\nline":    `'new\nline'`,
	}

	for in, want := range cases {
		if got := quoteString(in); got != want {
			t.Errorf("quoteString(%q) = %s, want %s", in, got, want)
		}
	}
}
