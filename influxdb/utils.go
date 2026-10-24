package influxdb

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func hashSum(contents interface{}) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents.(string))))
}

var stringLiteralEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)

// quoteString returns s as a single-quoted InfluxQL string literal, like influxql.QuoteString.
func quoteString(s string) string {
	return "'" + stringLiteralEscaper.Replace(s) + "'"
}
