package knowledge

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

const APIVersion = "2024-07-01"

type Result struct {
	Content string `json:"Content"`
	Title   string `json:"Title"`
}

type Client struct {
	Endpoint string
	Index    string
	HTTP     *http.Client
	Identity cloud.TokenSource
}

func (c *Client) Call(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.Identity.Token(ctx, "https://search.azure.com/.default")
	if err != nil {
		return err
	}
	var data []byte
	if body != nil {
		data, err = cloud.JSON(body)
		if err != nil {
			return err
		}
	}
	return cloud.Do(ctx, c.HTTP, "search", method, cloud.Base(c.Endpoint)+path+"?api-version="+APIVersion, cloud.Bearer(token), data, out)
}

func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	var response struct {
		Value *[]struct {
			Content  string `json:"content"`
			TitleB64 string `json:"title_b64"`
		} `json:"value"`
	}
	err := c.Call(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.Index)+"/docs/search", map[string]any{
		"search": query, "queryType": "simple", "searchMode": "any", "top": 5,
		"searchFields": "content", "select": "content,title_b64",
	}, &response)
	if err != nil {
		return nil, err
	}
	if response.Value == nil {
		return nil, errors.New("search: incomplete query response")
	}
	results := make([]Result, 0, len(*response.Value))
	for _, item := range *response.Value {
		title, err := base64.StdEncoding.DecodeString(item.TitleB64)
		if err != nil || len(title) == 0 || !utf8.Valid(title) || strings.TrimSpace(item.Content) == "" || utf8.RuneCountInString(item.Content) > 4000 {
			return nil, errors.New("search: index content does not match the approved chunk schema")
		}
		results = append(results, Result{Content: item.Content, Title: string(title)})
	}
	return results, nil
}

func (c *Client) RunIndexer(ctx context.Context) error {
	return c.Call(ctx, http.MethodPost, "/indexers/"+url.PathEscape(c.Index+"-indexer")+"/run", nil, nil)
}
