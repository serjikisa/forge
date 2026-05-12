package tool

import (
	"net/http"
	"strings"
	"time"
)

// sharedClient is a reusable HTTP client for web tools.
var sharedClient = &http.Client{Timeout: 20 * time.Second}

// decodeHTMLEntities replaces common HTML entities with their characters.
func decodeHTMLEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#x27;", "'",
		"&#39;", "'",
		"&nbsp;", " ",
	)
	return r.Replace(s)
}
