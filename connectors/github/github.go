package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/saitejasrivilli/glean-lite/internal/connector"
)

const apiBase = "https://api.github.com"

// Connector fetches files from one or more GitHub repositories.
type Connector struct {
	token string
	repos []string // e.g. ["yourname/glean-lite", "yourname/other-repo"]
}

func New() *Connector {
	repos := strings.Split(os.Getenv("GITHUB_REPOS"), ",")
	return &Connector{
		token: os.Getenv("GITHUB_TOKEN"),
		repos: repos,
	}
}

func (c *Connector) Name() string { return "github" }

func (c *Connector) Fetch(ctx context.Context) ([]connector.Document, error) {
	var docs []connector.Document
	for _, repo := range c.repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		files, err := c.fetchTree(ctx, repo)
		if err != nil {
			return nil, fmt.Errorf("github: repo %s: %w", repo, err)
		}
		docs = append(docs, files...)
	}
	return docs, nil
}

type treeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"tree"`
}

func (c *Connector) fetchTree(ctx context.Context, repo string) ([]connector.Document, error) {
	url := fmt.Sprintf("%s/repos/%s/git/trees/HEAD?recursive=1", apiBase, repo)
	body, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}

	var tree treeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, err
	}

	var docs []connector.Document
	for _, item := range tree.Tree {
		if item.Type != "blob" {
			continue
		}
		if !isTextFile(item.Path) {
			continue
		}
		content, err := c.fetchBlob(ctx, item.URL)
		if err != nil {
			continue // skip unreadable blobs
		}
		docs = append(docs, connector.Document{
			ID:    fmt.Sprintf("github:%s:%s", repo, item.Path),
			Title: item.Path,
			Body:  content,
			URL:   fmt.Sprintf("https://github.com/%s/blob/HEAD/%s", repo, item.Path),
			Metadata: map[string]string{
				"repo":   repo,
				"source": "github",
				"path":   item.Path,
			},
		})
	}
	return docs, nil
}

type blobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (c *Connector) fetchBlob(ctx context.Context, url string) (string, error) {
	body, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	var blob blobResponse
	if err := json.Unmarshal(body, &blob); err != nil {
		return "", err
	}
	if blob.Encoding != "base64" {
		return blob.Content, nil
	}
	// GitHub base64 has newlines — strip them
	clean := strings.ReplaceAll(blob.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (c *Connector) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github API %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// isTextFile returns true for file types worth indexing.
func isTextFile(path string) bool {
	skipPrefixes := []string{"vendor/", "node_modules/", ".git/"}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	textExts := []string{
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs",
		".md", ".txt", ".yaml", ".yml", ".json", ".toml",
		".sh", ".sql", ".html", ".css",
	}
	lower := strings.ToLower(path)
	for _, ext := range textExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
