package influxdb

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
	"github.com/influxdata/influxql"
)

// hashSum is the SHA-256 hex digest that provider versions before 2.0 stored
// in state instead of the user password.
func hashSum(contents string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
}

var stringLiteralEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)

// quoteString returns s as a single-quoted InfluxQL string literal, like influxql.QuoteString.
func quoteString(s string) string {
	return "'" + stringLiteralEscaper.Replace(s) + "'"
}

// firstSeriesValues returns the rows of the first series of the first result, or nil when the
// query returned no series (e.g. SHOW USERS on a server without users).
func firstSeriesValues(resp *client.Response) [][]interface{} {
	if resp == nil || len(resp.Results) == 0 || len(resp.Results[0].Series) == 0 {
		return nil
	}

	return resp.Results[0].Series[0].Values
}

// keepEquivalentDuration returns the prior duration string when it denotes the
// same duration as the one InfluxDB reports (which it normalises, e.g. 1d to
// 24h0m0s), so the configured spelling is kept in state; otherwise reported.
func keepEquivalentDuration(prior types.String, reported string) string {
	if prior.IsNull() || prior.IsUnknown() {
		return reported
	}
	a, errA := parseInfluxDuration(prior.ValueString())
	b, errB := parseInfluxDuration(reported)
	if errA == nil && errB == nil && a == b {
		return prior.ValueString()
	}
	return reported
}

func parseInfluxDuration(s string) (int64, error) {
	if strings.EqualFold(s, "INF") {
		return 0, nil
	}
	// Handles InfluxQL units (d, w) and the compound form InfluxDB reports, e.g. 24h0m0s.
	d, err := influxql.ParseDuration(s)
	return int64(d), err
}
