package formward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultPostmarkBaseURL is the production endpoint. Tests override
	// PostmarkTransport.BaseURL with an httptest.NewServer URL.
	defaultPostmarkBaseURL = "https://api.postmarkapp.com"

	// postmarkSendPath is the single endpoint used in v1.0 (single-recipient
	// send). Batch send is deferred.
	postmarkSendPath = "/email"

	// postmarkRequestTimeout is the per-request hard cap on the HTTP client.
	// The caller's context handles the overall deadline; this is a defensive
	// upper bound so a hung TCP connection cannot block Send forever.
	postmarkRequestTimeout = 5 * time.Second

	// postmarkResponseSizeLimit caps the response body we read into memory
	// even on error paths. Postmark errors are tiny JSON; this prevents a
	// hostile or misbehaving upstream from forcing us to allocate.
	postmarkResponseSizeLimit = 64 * 1024
)

// postmarkHTTPClient is the package-level HTTP client used by every
// PostmarkTransport. Sharing one client lets the connection pool amortize
// across config reloads and across multiple `formward` directives in a
// single Caddy config.
var postmarkHTTPClient = &http.Client{
	Timeout: postmarkRequestTimeout,
}

// PostmarkTransport implements Transport against Postmark's JSON HTTP API.
//
// Per ADR-1: bespoke client, no third-party Postmark SDK. The surface is
// small enough that the dependency cost (version pinning, security tracking,
// API drift) outweighs the LOC saved.
//
// NFR1 enforcement: every submitter-controlled value reaches Postmark
// exclusively through json.Marshal of a struct with named fields. There is
// no path in this file that constructs HTTP headers or email headers via
// string concatenation of user input.
type PostmarkTransport struct {
	// APIKey is the Postmark Server Token. Sent via the
	// X-Postmark-Server-Token header. Never logged (NFR3).
	APIKey string

	// BaseURL is the API base. Empty means use the production default.
	// Tests set this to an httptest.NewServer URL; production never sets it.
	BaseURL string
}

// NewPostmarkTransport constructs a transport. baseURL may be empty.
func NewPostmarkTransport(apiKey, baseURL string) *PostmarkTransport {
	if baseURL == "" {
		baseURL = defaultPostmarkBaseURL
	}
	return &PostmarkTransport{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}
}

// postmarkRequest is the JSON payload sent to /email. Field names match
// Postmark's spec exactly. Every field is forwarded as opaque data; Postmark
// handles header construction on their end.
//
// SECURITY: adding a field here is a security-relevant change. The set of
// fields defines what submitter-controlled data crosses to Postmark. Review
// NFR1 implications before adding (especially anything that might let
// callers smuggle structural data, e.g., a Headers []map field).
type postmarkRequest struct {
	From     string `json:"From"`
	To       string `json:"To"`
	ReplyTo  string `json:"ReplyTo,omitempty"`
	Subject  string `json:"Subject"`
	TextBody string `json:"TextBody"`
}

// postmarkResponse captures the fields we read from Postmark's JSON reply.
// Other fields (MessageID, SubmittedAt, etc.) are intentionally ignored.
type postmarkResponse struct {
	ErrorCode int    `json:"ErrorCode"`
	Message   string `json:"Message"`
}

// Send implements Transport.
//
// Status code mapping (FR18-20):
//
//	200, 202   → success
//	429        → ErrRateLimited
//	5xx        → ErrTransient
//	4xx (other)→ ErrTerminal
//	network/timeout/ctx → ErrTransient (caller will retry once)
func (p *PostmarkTransport) Send(ctx context.Context, msg Message) error {
	body := postmarkRequest{
		From:     msg.From,
		To:       strings.Join(msg.To, ", "),
		ReplyTo:  msg.ReplyTo,
		Subject:  msg.Subject,
		TextBody: msg.BodyText,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		// json.Marshal of a struct with only string/[]string fields cannot
		// fail in practice; if it ever does, treat as terminal.
		return &TransportError{
			Class:   ErrTerminal,
			Cause:   err,
			Message: "encode postmark request",
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+postmarkSendPath, bytes.NewReader(buf))
	if err != nil {
		return &TransportError{
			Class:   ErrTerminal,
			Cause:   err,
			Message: "build postmark request",
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.APIKey)

	resp, err := postmarkHTTPClient.Do(req)
	if err != nil {
		// DNS failure, connection refused, context cancel/deadline,
		// or client timeout. All map to transient.
		return &TransportError{
			Class:   ErrTransient,
			Cause:   err,
			Message: "postmark request failed",
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, postmarkResponseSizeLimit))

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted:
		return nil

	case resp.StatusCode == http.StatusTooManyRequests:
		return &TransportError{
			Class:   ErrRateLimited,
			Status:  resp.StatusCode,
			Message: postmarkErrorMessage(respBody, "postmark rate limit"),
		}

	case resp.StatusCode >= 500:
		return &TransportError{
			Class:   ErrTransient,
			Status:  resp.StatusCode,
			Message: postmarkErrorMessage(respBody, "postmark server error"),
		}

	case resp.StatusCode >= 400:
		return &TransportError{
			Class:   ErrTerminal,
			Status:  resp.StatusCode,
			Message: postmarkErrorMessage(respBody, "postmark rejected request"),
		}

	default:
		// 1xx, 3xx, anything else unexpected.
		return &TransportError{
			Class:   ErrTerminal,
			Status:  resp.StatusCode,
			Message: fmt.Sprintf("unexpected postmark status %d", resp.StatusCode),
		}
	}
}

// postmarkErrorMessage extracts the Message field from a Postmark error
// response if it parses as JSON. Falls back to a generic operator-facing
// string. The result is logged and may appear in 502 response bodies (only
// the generic phrasing is exposed to clients per architectural open Q5);
// it MUST NOT include the API key. Since this function only ever reads the
// upstream response body, never the request, that holds by construction.
func postmarkErrorMessage(body []byte, fallback string) string {
	var resp postmarkResponse
	if err := json.Unmarshal(body, &resp); err != nil || resp.Message == "" {
		return fallback
	}
	return resp.Message
}

var _ Transport = (*PostmarkTransport)(nil)
