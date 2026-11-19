// Package webuitest holds the test helpers and the shared scenarios of the
// debug web UI. The page package dogerhttp and every adapter module run the
// same scenarios through it, each with its own way to mount the UI.
//
// It is internal: only the dogerhttp tests and the adapter tests import it.
package webuitest

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BasePath is the path every scenario mounts the UI at.
const BasePath = "/debug/doger"

// Doer sends one request to a mounted UI and returns its response.
type Doer func(req *http.Request) *http.Response

// Mount mounts the UI for root at BasePath and returns a Doer for it.
type Mount func(t *testing.T, root doger.Injector) Doer

// HandlerDoer returns a Doer that serves each request with h.
func HandlerDoer(h http.Handler) Doer {
	return func(req *http.Request) *http.Response {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec.Result()
	}
}

// Response is a response read in full.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       string
}

// Do sends req with do and returns its response, read in full.
func Do(t *testing.T, do Doer, req *http.Request) Response {
	t.Helper()

	resp := do(req)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: string(body)}
}

// Get sends GET target with do and returns its response, read in full.
// target may be an unescaped link taken from a page, such as one holding a
// space: it is parsed as a URL, not as a request line.
func Get(t *testing.T, do Doer, target string) Response {
	t.Helper()

	return Do(t, do, NewRequest(t, http.MethodGet, target))
}

// NewRequest returns a server request for method and target.
func NewRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()

	u, err := url.Parse(target)
	require.NoError(t, err)

	req := httptest.NewRequest(method, "/", http.NoBody)
	req.URL = u
	req.RequestURI = u.RequestURI()

	return req
}

// Tag is an HTML start tag found in a page.
type Tag struct {
	Name       string
	Attributes []string
}

var (
	tagRe       = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9-]*)([^>]*)>`)
	attributeRe = regexp.MustCompile(`([^\s"'=<>/]+)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+))?`)
	linkRe      = regexp.MustCompile(`(?s)<a\s[^>]*?href="([^"]*)"[^>]*>(.*?)</a>`)
)

// Tags returns every start tag of page with its attribute names, lowercased.
// Text a page escapes holds no "<", so every "<" followed by a letter starts
// a real element.
func Tags(page string) []Tag {
	matches := tagRe.FindAllStringSubmatch(page, -1)
	tags := make([]Tag, 0, len(matches))

	for _, m := range matches {
		tag := Tag{Name: strings.ToLower(m[1])}
		for _, a := range attributeRe.FindAllStringSubmatch(m[2], -1) {
			tag.Attributes = append(tag.Attributes, strings.ToLower(a[1]))
		}

		tags = append(tags, tag)
	}

	return tags
}

// injectedElements are the elements no page renders, so finding one means a
// name injected it.
var injectedElements = []string{"script", "img"}

// AssertNoInjectedMarkup asserts that page holds no "script" or "img"
// element and no attribute whose name starts with "on".
func AssertNoInjectedMarkup(t *testing.T, page string) {
	t.Helper()

	for _, tag := range Tags(page) {
		assert.NotContains(t, injectedElements, tag.Name, "injected element")

		for _, attribute := range tag.Attributes {
			assert.False(t, strings.HasPrefix(attribute, "on"), "injected attribute %q on <%s>", attribute, tag.Name)
		}
	}
}

// Link is an anchor of a page, with its href and text unescaped.
type Link struct {
	Href string
	Text string
}

// Links returns every anchor of page.
func Links(page string) []Link {
	matches := linkRe.FindAllStringSubmatch(page, -1)
	links := make([]Link, 0, len(matches))

	for _, m := range matches {
		links = append(links, Link{
			Href: html.UnescapeString(m[1]),
			Text: strings.TrimSpace(html.UnescapeString(m[2])),
		})
	}

	return links
}

// LinkByText returns the first anchor of page whose text is text.
func LinkByText(t *testing.T, page, text string) Link {
	t.Helper()

	for _, link := range Links(page) {
		if link.Text == text {
			return link
		}
	}

	require.FailNowf(t, "link not found", "no link with text %q", text)

	return Link{}
}
