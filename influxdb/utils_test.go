package influxdb

import "testing"

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
