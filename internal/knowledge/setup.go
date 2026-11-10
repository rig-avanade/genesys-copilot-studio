package knowledge

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) Setup(ctx context.Context, storageResourceID, container string) error {
	index := map[string]any{
		"name": c.Index,
		"fields": []any{
			map[string]any{"name": "id", "type": "Edm.String", "key": true, "searchable": true, "analyzer": "keyword"},
			map[string]any{"name": "parent_id", "type": "Edm.String", "filterable": true},
			map[string]any{"name": "content", "type": "Edm.String", "searchable": true, "retrievable": true},
			map[string]any{"name": "title_b64", "type": "Edm.String", "retrievable": true},
		},
	}
	datasource := map[string]any{
		"name": c.Index + "-source", "type": "azureblob",
		"credentials": map[string]string{"connectionString": "ResourceId=" + storageResourceID + ";"},
		"container":   map[string]string{"name": container, "query": "approved/"},
		"dataDeletionDetectionPolicy": map[string]string{
			"@odata.type": "#Microsoft.Azure.Search.NativeBlobSoftDeleteDeletionDetectionPolicy",
		},
	}
	skillset := map[string]any{
		"name": c.Index + "-split",
		"skills": []any{map[string]any{
			"@odata.type": "#Microsoft.Skills.Text.SplitSkill", "name": "split", "context": "/document",
			"textSplitMode": "pages", "maximumPageLength": 2000, "pageOverlapLength": 200,
			"inputs":  []any{map[string]string{"name": "text", "source": "/document/content"}},
			"outputs": []any{map[string]string{"name": "textItems", "targetName": "chunks"}},
		}},
		"indexProjections": map[string]any{
			"selectors": []any{map[string]any{
				"targetIndexName": c.Index, "parentKeyFieldName": "parent_id", "sourceContext": "/document/chunks/*",
				"mappings": []any{
					map[string]string{"name": "content", "source": "/document/chunks/*"},
					map[string]string{"name": "title_b64", "source": "/document/title_b64"},
				},
			}},
			"parameters": map[string]string{"projectionMode": "skipIndexingParentDocuments"},
		},
	}
	indexer := map[string]any{
		"name": c.Index + "-indexer", "dataSourceName": c.Index + "-source",
		"targetIndexName": c.Index, "skillsetName": c.Index + "-split",
		"schedule": map[string]string{"interval": "PT15M"},
		"parameters": map[string]any{
			"maxFailedItems": 0, "maxFailedItemsPerBatch": 0,
			"configuration": map[string]any{
				"dataToExtract": "contentAndMetadata", "parsingMode": "default",
				"failOnUnsupportedContentType": true, "failOnUnprocessableDocument": true,
			},
		},
	}
	for _, resource := range []struct {
		path string
		body any
	}{
		{"/indexes/" + url.PathEscape(c.Index), index},
		{"/datasources/" + url.PathEscape(c.Index+"-source"), datasource},
		{"/skillsets/" + url.PathEscape(c.Index+"-split"), skillset},
		{"/indexers/" + url.PathEscape(c.Index+"-indexer"), indexer},
	} {
		if err := c.Call(ctx, http.MethodPut, resource.path, resource.body, nil); err != nil {
			return err
		}
	}
	return nil
}
