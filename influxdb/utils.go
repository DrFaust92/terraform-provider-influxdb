package influxdb

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/influxdata/influxdb/client"
)

func hashSum(contents interface{}) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents.(string))))
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
