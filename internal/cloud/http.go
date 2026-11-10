package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenSource interface {
	Token(context.Context, string) (string, error)
}

type HTTPError struct {
	Service string
	Status  int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", e.Service, e.Status)
}

func IsStatus(err error, status int) bool {
	var target *HTTPError
	return errors.As(err, &target) && target.Status == status
}

func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func HTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("expected an absolute HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

// Do never returns upstream bodies or URLs in errors; they can contain credentials or customer data.
func Do(ctx context.Context, client *http.Client, service, method, endpoint string, headers http.Header, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: invalid request URL", service)
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: network request failed", service)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Service: service, Status: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return fmt.Errorf("%s: response could not be read within size limit", service)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: invalid JSON response", service)
	}
	return nil
}

func JSON(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("cannot encode request")
	}
	return b, nil
}

func Bearer(token string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+token)
	return h
}

func Base(raw string) string { return strings.TrimRight(raw, "/") }
