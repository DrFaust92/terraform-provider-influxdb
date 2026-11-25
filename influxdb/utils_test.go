package influxdb

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

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

func TestKeepEquivalentDuration(t *testing.T) {
	cases := []struct {
		prior    types.String
		reported string
		want     string
	}{
		{types.StringValue("1d"), "24h0m0s", "1d"},
		{types.StringValue("2w"), "336h0m0s", "2w"},
		{types.StringValue("INF"), "0s", "INF"},
		{types.StringValue("24h0m0s"), "24h0m0s", "24h0m0s"},
		{types.StringValue("1d"), "48h0m0s", "48h0m0s"},
		{types.StringNull(), "24h0m0s", "24h0m0s"},
		{types.StringValue("bogus"), "24h0m0s", "24h0m0s"},
	}

	for _, c := range cases {
		if got := keepEquivalentDuration(c.prior, c.reported); got != c.want {
			t.Errorf("keepEquivalentDuration(%s, %q) = %q, want %q", c.prior, c.reported, got, c.want)
		}
	}
}
