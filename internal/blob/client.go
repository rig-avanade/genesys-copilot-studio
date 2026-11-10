package blob

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

var ErrBusy = errors.New("storage lease is already held")

type Client struct {
	Endpoint  string
	Container string
	HTTP      *http.Client
	Identity  cloud.TokenSource
}

func (c *Client) endpoint(name string, query url.Values) string {
	parts := strings.Split(name, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	target := cloud.Base(c.Endpoint) + "/" + url.PathEscape(c.Container)
	if name != "" {
		target += "/" + strings.Join(parts, "/")
	}
	if len(query) != 0 {
		target += "?" + query.Encode()
	}
	return target
}

func (c *Client) request(ctx context.Context, method, name string, query url.Values, headers http.Header, data []byte) (*http.Response, error) {
	token, err := c.Identity.Token(ctx, "https://storage.azure.com/.default")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(name, query), bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("storage: invalid request")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-ms-version", "2023-11-03")
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("storage: network request failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, &cloud.HTTPError{Service: "storage", Status: resp.StatusCode}
	}
	return resp, nil
}

func (c *Client) Get(ctx context.Context, name string) ([]byte, error) {
	resp, err := c.request(ctx, http.MethodGet, name, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("storage: invalid session payload")
	}
	return data, nil
}

func (c *Client) Put(ctx context.Context, name string, data []byte, headers http.Header) error {
	h := headers.Clone()
	if h == nil {
		h = make(http.Header)
	}
	h.Set("x-ms-blob-type", "BlockBlob")
	resp, err := c.request(ctx, http.MethodPut, name, nil, h, data)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (c *Client) Delete(ctx context.Context, name string) error {
	resp, err := c.request(ctx, http.MethodDelete, name, nil, nil, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type Item struct {
	Name     string `xml:"Name"`
	Metadata struct {
		SourceETag string `xml:"sourceetag"`
	} `xml:"Metadata"`
}

func (c *Client) List(ctx context.Context, prefix string) ([]Item, error) {
	var items []Item
	marker := ""
	seen := map[string]bool{}
	for {
		q := url.Values{"restype": {"container"}, "comp": {"list"}, "prefix": {prefix}, "include": {"metadata"}}
		if marker != "" {
			q.Set("marker", marker)
		}
		resp, err := c.request(ctx, http.MethodGet, "", q, nil, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items []Item `xml:"Blobs>Blob"`
			Next  string `xml:"NextMarker"`
		}
		err = xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, errors.New("storage: invalid list response")
		}
		for _, item := range page.Items {
			if !strings.HasPrefix(item.Name, prefix) {
				return nil, errors.New("storage: listing returned a blob outside the approved prefix")
			}
		}
		items = append(items, page.Items...)
		if page.Next == "" {
			return items, nil
		}
		if seen[page.Next] {
			return nil, errors.New("storage: repeated continuation marker")
		}
		seen[page.Next] = true
		marker = page.Next
	}
}

type Lease struct {
	ID     string
	Name   string
	client *Client
}

func (l *Lease) Put(ctx context.Context, data []byte) error {
	h := make(http.Header)
	h.Set("x-ms-lease-id", l.ID)
	return l.client.Put(ctx, l.Name, data, h)
}

func (c *Client) lease(ctx context.Context, name, action, id string) (string, error) {
	h := make(http.Header)
	h.Set("x-ms-lease-action", action)
	if action == "acquire" {
		h.Set("x-ms-lease-duration", "60")
	} else {
		h.Set("x-ms-lease-id", id)
	}
	resp, err := c.request(ctx, http.MethodPut, name, url.Values{"comp": {"lease"}}, h, nil)
	if err != nil {
		if action == "acquire" && cloud.IsStatus(err, http.StatusConflict) {
			return "", ErrBusy
		}
		return "", err
	}
	defer resp.Body.Close()
	if action == "acquire" && resp.Header.Get("x-ms-lease-id") == "" {
		return "", errors.New("storage: missing lease identifier")
	}
	return resp.Header.Get("x-ms-lease-id"), nil
}

// WithLease serializes all writers, including across App Service instances.
func (c *Client) WithLease(ctx context.Context, name string, fn func(context.Context, *Lease) error) error {
	h := make(http.Header)
	h.Set("If-None-Match", "*")
	err := c.Put(ctx, name, []byte("{}"), h)
	if err != nil && !cloud.IsStatus(err, http.StatusPreconditionFailed) && !cloud.IsStatus(err, http.StatusConflict) {
		return err
	}
	id, err := c.lease(ctx, name, "acquire", "")
	if err != nil {
		return err
	}
	workCtx, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				finished <- nil
				return
			case <-ticker.C:
				if _, err := c.lease(workCtx, name, "renew", id); err != nil {
					if workCtx.Err() != nil && ctx.Err() == nil {
						finished <- nil
						return
					}
					cancel()
					finished <- fmt.Errorf("lease renewal: %w", err)
					return
				}
			}
		}
	}()
	workErr := fn(workCtx, &Lease{ID: id, Name: name, client: c})
	cancel()
	renewErr := <-finished
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer releaseCancel()
	_, releaseErr := c.lease(releaseCtx, name, "release", id)
	return errors.Join(workErr, renewErr, releaseErr)
}
