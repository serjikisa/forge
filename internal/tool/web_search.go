// web_search.go implements a web search tool using DuckDuckGo's HTML interface.
// No API key required — uses the lite HTML endpoint and parses results.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type WebSearch struct{}

func (w *WebSearch) Name() string        { return "web_search" }
func (w *WebSearch) Description() string { return "Search the web using DuckDuckGo and return results" }
func (w *WebSearch) Safety() SafetyLevel { return NeedsConfirmation }
func (w *WebSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"search query"},"num_results":{"type":"integer","description":"max results to return (default 5)"}},"required":["query"]}`)
}

func (w *WebSearch) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Query      string          `json:"query"`
		NumResults flexInt         `json:"num_results"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.NumResults <= 0 {
		p.NumResults = 5
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(p.Query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Forge/1.0)")

	resp, err := sharedClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	results := parseDDGResults(string(body), int(p.NumResults))
	if len(results) == 0 {
		return "No results found for: " + p.Query, nil
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, r.title, r.url, r.snippet)
	}
	return sb.String(), nil
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

var (
	resultLinkRe = regexp.MustCompile(`<a[^>]+class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetRe    = regexp.MustCompile(`<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
)

func parseDDGResults(html string, max int) []searchResult {
	links := resultLinkRe.FindAllStringSubmatch(html, max)
	snippets := snippetRe.FindAllStringSubmatch(html, max)

	var results []searchResult
	for i, link := range links {
		if i >= max {
			break
		}
		r := searchResult{
			title: stripTags(link[2]),
			url:   extractDDGURL(link[1]),
		}
		if i < len(snippets) {
			r.snippet = stripTags(snippets[i][1])
		}
		results = append(results, r)
	}
	return results
}

func extractDDGURL(raw string) string {
	// DDG wraps URLs in a redirect: //duckduckgo.com/l/?uddg=<encoded>&...
	if strings.Contains(raw, "uddg=") {
		if u, err := url.Parse(raw); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	return raw
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(decodeHTMLEntities(s))
}
