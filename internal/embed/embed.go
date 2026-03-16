package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Model used for embeddings — 384-dimensional, fast, free on HuggingFace
const defaultModel = "sentence-transformers/all-MiniLM-L6-v2"

// Client wraps the HuggingFace Inference API for text embeddings.
type Client struct {
	apiKey string
	model  string
}

func NewClient() *Client {
	model := os.Getenv("EMBED_MODEL")
	if model == "" {
		model = defaultModel
	}
	return &Client{
		apiKey: os.Getenv("HF_API_KEY"),
		model:  model,
	}
}

// Embed returns a vector for a single string.
func (c *Client) Embed(text string) ([]float32, error) {
	vecs, err := c.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch returns vectors for multiple strings in one API call.
func (c *Client) EmbedBatch(texts []string) ([][]float32, error) {
	payload := map[string]any{"inputs": texts}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://router.huggingface.co/hf-inference/models/%s/pipeline/feature-extraction", c.model)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HuggingFace embed error %d: %s", resp.StatusCode, string(raw))
	}

	var vectors [][]float32
	if err := json.Unmarshal(raw, &vectors); err != nil {
		return nil, fmt.Errorf("failed to parse embeddings: %w", err)
	}
	return vectors, nil
}
