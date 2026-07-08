package quark

import (
	"io"
	"net/http"
	"strings"
)

// Doer abstracts the HTTP transport so unit tests can inject a mock.
//
// Production code uses *http.Client to satisfy this interface; unit tests use
// FakeDoer to record requests and return canned responses.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Compile-time check that *http.Client implements Doer.
var _ Doer = (*http.Client)(nil)

// FakeDoer is a Doer implementation for unit tests that matches by URL and
// returns preset responses.
//
// Usage:
//
//	f := &FakeDoer{}
//	f.Register("GET", "/account/info", 200, `{"success":true,"code":0,...}`)
//	client := NewClient(WithDoer(f))
type FakeDoer struct {
	// Responses indexes preset responses by "METHOD URL".
	Responses map[string]FakeResponse
	// Calls records every invocation for assertion.
	Calls []FakeCall
}

// FakeResponse is a preset response.
type FakeResponse struct {
	Status int
	Body   string
	// Header holds extra response headers (e.g. set-cookie).
	Header http.Header
}

// FakeCall records a single invocation.
type FakeCall struct {
	Method string
	URL    string
	Header http.Header
}

// Register registers a preset response. urlMatch supports substring matching
// (a hit when the request URL contains it).
func (f *FakeDoer) Register(method, urlMatch string, status int, body string) {
	if f.Responses == nil {
		f.Responses = make(map[string]FakeResponse)
	}
	f.Responses[method+" "+urlMatch] = FakeResponse{Status: status, Body: body, Header: http.Header{}}
}

// RegisterWithHeader registers a preset response with extra response headers
// (e.g. set-cookie).
func (f *FakeDoer) RegisterWithHeader(method, urlMatch string, status int, body string, header http.Header) {
	if f.Responses == nil {
		f.Responses = make(map[string]FakeResponse)
	}
	f.Responses[method+" "+urlMatch] = FakeResponse{Status: status, Body: body, Header: header}
}

// Do implements Doer.
func (f *FakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.Calls = append(f.Calls, FakeCall{
		Method: req.Method,
		URL:    req.URL.String(),
		Header: req.Header.Clone(),
	})
	// Prefer an exact match (full URL including query), then fall back to
	// substring matching.
	fullURL := req.URL.Path
	if req.URL.RawQuery != "" {
		fullURL += "?" + req.URL.RawQuery
	}
	key := req.Method + " " + fullURL
	resp, ok := f.Responses[key]
	if !ok {
		// Substring match: iterate in registration order, take the first hit.
		for k, v := range f.Responses {
			idx := strings.IndexByte(k, ' ')
			if idx < 0 {
				continue
			}
			m, u := k[:idx], k[idx+1:]
			if m == req.Method && strings.Contains(fullURL, u) {
				resp = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return &http.Response{StatusCode: 404, Body: http.NoBody, Header: http.Header{}}, nil
	}
	h := resp.Header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{
		StatusCode: resp.Status,
		Body:       io.NopCloser(strings.NewReader(resp.Body)),
		Header:     h,
	}, nil
}
