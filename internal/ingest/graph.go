package ingest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

const MaxFileBytes = 16 << 20

type File struct {
	ID, Name, ETag string
	Size           int64
}

type Source interface {
	List(context.Context) ([]File, error)
	Download(context.Context, File) ([]byte, error)
}

type Graph struct {
	Endpoint       string
	DriveID        string
	FolderID       string
	SharePointHost string
	HTTP           *http.Client
	Identity       cloud.TokenSource
}

func supported(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf", ".docx", ".pptx", ".xlsx", ".txt", ".md", ".html", ".htm":
		return true
	default:
		return false
	}
}

func (g *Graph) itemPath(id string) string {
	return "/drives/" + url.PathEscape(g.DriveID) + "/items/" + url.PathEscape(id)
}

func (g *Graph) List(ctx context.Context) ([]File, error) {
	queue := []string{g.FolderID}
	seen := map[string]bool{g.FolderID: true}
	files := []File{}
	base, err := url.Parse(cloud.Base(g.Endpoint))
	if err != nil {
		return nil, errors.New("graph: invalid endpoint")
	}
	for len(queue) > 0 {
		folderID := queue[0]
		queue = queue[1:]
		next := cloud.Base(g.Endpoint) + g.itemPath(folderID) + "/children?$select=id,name,size,eTag,file,folder,remoteItem"
		pages := map[string]bool{}
		for next != "" {
			link, err := url.Parse(next)
			if err != nil || link.Scheme != base.Scheme || link.Host != base.Host || link.User != nil ||
				!strings.HasPrefix(link.Path, base.Path+"/") || pages[next] {
				return nil, errors.New("graph: invalid or repeated pagination link")
			}
			pages[next] = true
			token, err := g.Identity.Token(ctx, "https://graph.microsoft.com/.default")
			if err != nil {
				return nil, err
			}
			var page struct {
				Value *[]struct {
					ID     string    `json:"id"`
					Name   string    `json:"name"`
					ETag   string    `json:"eTag"`
					Size   int64     `json:"size"`
					File   *struct{} `json:"file"`
					Folder *struct{} `json:"folder"`
					Remote *struct{} `json:"remoteItem"`
				} `json:"value"`
				Next string `json:"@odata.nextLink"`
			}
			if err := cloud.Do(ctx, g.HTTP, "graph", http.MethodGet, next, cloud.Bearer(token), nil, &page); err != nil {
				return nil, err
			}
			if page.Value == nil {
				return nil, errors.New("graph: incomplete folder listing")
			}
			for _, item := range *page.Value {
				if item.ID == "" || seen[item.ID] || item.Remote != nil || len(seen) >= 10000 {
					return nil, errors.New("graph: invalid, linked, repeated, or excessive folder item")
				}
				seen[item.ID] = true
				switch {
				case item.Folder != nil:
					queue = append(queue, item.ID)
				case item.File != nil:
					if !supported(item.Name) || item.Size <= 0 || item.Size > MaxFileBytes || item.ETag == "" {
						return nil, errors.New("graph: source contains an unsupported, empty, oversized, or unversioned file")
					}
					files = append(files, File{ID: item.ID, Name: item.Name, ETag: item.ETag, Size: item.Size})
				default:
					return nil, errors.New("graph: unsupported folder item type")
				}
			}
			next = page.Next
		}
	}
	return files, nil
}

func (g *Graph) Download(ctx context.Context, file File) ([]byte, error) {
	token, err := g.Identity.Token(ctx, "https://graph.microsoft.com/.default")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloud.Base(g.Endpoint)+g.itemPath(file.ID)+"/content", nil)
	if err != nil {
		return nil, errors.New("graph: invalid download request")
	}
	req.Header = cloud.Bearer(token)
	req.Header.Set("If-Match", file.ETag)
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("graph: file download request failed")
	}
	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		resp.Body.Close()
		u, err := url.Parse(location)
		if err != nil || u.Scheme != "https" || u.Host != g.SharePointHost || u.User != nil {
			return nil, errors.New("graph: download redirect does not match SHAREPOINT_HOST")
		}
		// The redirect URL is preauthenticated. Never forward the Graph bearer token.
		download, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, errors.New("graph: invalid preauthenticated download URL")
		}
		resp, err = g.HTTP.Do(download)
		if err != nil {
			return nil, errors.New("sharepoint: file download failed")
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &cloud.HTTPError{Service: "sharepoint download", Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxFileBytes || int64(len(data)) != file.Size {
		return nil, errors.New("sharepoint: file is empty, oversized, or changed during download")
	}
	return data, nil
}
