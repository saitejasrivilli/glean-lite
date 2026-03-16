package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pb "github.com/qdrant/go-client/qdrant"
	"github.com/saitejasrivilli/glean-lite/connectors/github"
	"github.com/saitejasrivilli/glean-lite/internal/connector"
	"github.com/saitejasrivilli/glean-lite/internal/embed"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	collectionName = "glean-lite"
	vectorSize     = 384
	topK           = 5
)

type Engine struct {
	qdrant     pb.PointsClient
	collection pb.CollectionsClient
	embedder   *embed.Client
	groqKey    string
	qdrantKey  string
	connectors []connector.Connector
}

func NewEngine() (*Engine, error) {
	qdrantAddr := os.Getenv("QDRANT_ADDR")
	if qdrantAddr == "" {
		qdrantAddr = "localhost:6334"
	}
	qdrantKey := os.Getenv("QDRANT_API_KEY")

	conn, err := grpc.Dial(qdrantAddr,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("qdrant dial: %w", err)
	}

	e := &Engine{
		qdrant:     pb.NewPointsClient(conn),
		collection: pb.NewCollectionsClient(conn),
		embedder:   embed.NewClient(),
		groqKey:    os.Getenv("GROQ_API_KEY"),
		qdrantKey:  qdrantKey,
		connectors: []connector.Connector{
			github.New(),
		},
	}

	if err := e.ensureCollection(e.authCtx(context.Background())); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) authCtx(ctx context.Context) context.Context {
	if e.qdrantKey == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "api-key", e.qdrantKey)
}

func (e *Engine) Router() chi.Router {
	r := chi.NewRouter()
	r.Post("/search", e.HandleSearch)
	r.Post("/index", e.HandleIndex)
	return r
}

type SearchRequest struct {
	Query string `json:"query"`
}

type SearchResult struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	URL      string            `json:"url"`
	Snippet  string            `json:"snippet"`
	Score    float32           `json:"score"`
	Metadata map[string]string `json:"metadata"`
}

type SearchResponse struct {
	Answer  string         `json:"answer"`
	Sources []SearchResult `json:"sources"`
}

func (e *Engine) HandleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}

	ctx := e.authCtx(r.Context())

	vec, err := e.embedder.Embed(req.Query)
	if err != nil {
		http.Error(w, "embedding failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	results, err := e.vectorSearch(ctx, vec)
	if err != nil {
		http.Error(w, "search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	answer, err := e.generateAnswer(ctx, req.Query, results)
	if err != nil {
		log.Printf("groq generation error: %v", err)
		answer = "Could not generate answer — showing raw results below."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SearchResponse{
		Answer:  answer,
		Sources: results,
	})
}

func (e *Engine) HandleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := e.authCtx(r.Context())
	total := 0

	for _, conn := range e.connectors {
		log.Printf("indexing from connector: %s", conn.Name())
		docs, err := conn.Fetch(ctx)
		if err != nil {
			log.Printf("connector %s fetch error: %v", conn.Name(), err)
			continue
		}

		if err := e.indexDocuments(ctx, docs); err != nil {
			http.Error(w, "index error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		total += len(docs)
		log.Printf("connector %s: indexed %d documents", conn.Name(), len(docs))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"indexed": total})
}

func (e *Engine) indexDocuments(ctx context.Context, docs []connector.Document) error {
	const batchSize = 50
	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		texts := make([]string, len(batch))
		for j, d := range batch {
			texts[j] = d.Title + "\n" + truncate(d.Body, 512)
		}

		vecs, err := e.embedder.EmbedBatch(texts)
		if err != nil {
			return fmt.Errorf("embed batch: %w", err)
		}

		points := make([]*pb.PointStruct, len(batch))
		for j, d := range batch {
			snippet := truncate(d.Body, 300)
			payload := map[string]*pb.Value{
				"id":      strVal(d.ID),
				"title":   strVal(d.Title),
				"url":     strVal(d.URL),
				"snippet": strVal(snippet),
				"source":  strVal(d.Metadata["source"]),
				"repo":    strVal(d.Metadata["repo"]),
			}
			points[j] = &pb.PointStruct{
				Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: uuid.New().String()}},
				Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: vecs[j]}}},
				Payload: payload,
			}
		}

		_, err = e.qdrant.Upsert(ctx, &pb.UpsertPoints{
			CollectionName: collectionName,
			Points:         points,
		})
		if err != nil {
			return fmt.Errorf("qdrant upsert: %w", err)
		}
	}
	return nil
}

func (e *Engine) vectorSearch(ctx context.Context, vec []float32) ([]SearchResult, error) {
	resp, err := e.qdrant.Search(ctx, &pb.SearchPoints{
		CollectionName: collectionName,
		Vector:         vec,
		Limit:          topK,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(resp.Result))
	for _, hit := range resp.Result {
		p := hit.Payload
		results = append(results, SearchResult{
			ID:      getStr(p, "id"),
			Title:   getStr(p, "title"),
			URL:     getStr(p, "url"),
			Snippet: getStr(p, "snippet"),
			Score:   hit.Score,
			Metadata: map[string]string{
				"source": getStr(p, "source"),
				"repo":   getStr(p, "repo"),
			},
		})
	}
	return results, nil
}

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
}

func (e *Engine) generateAnswer(ctx context.Context, query string, results []SearchResult) (string, error) {
	if e.groqKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY not set")
	}

	var ctxParts []string
	for i, r := range results {
		ctxParts = append(ctxParts, fmt.Sprintf("[%d] %s\n%s\nSource: %s", i+1, r.Title, r.Snippet, r.URL))
	}
	combinedCtx := strings.Join(ctxParts, "\n\n---\n\n")

	systemPrompt := `You are glean-lite, an intelligent search assistant.
Given context chunks retrieved from a codebase, answer the user's query concisely and accurately.
Cite sources using [1], [2] etc. If context is insufficient, say so honestly.`

	userPrompt := fmt.Sprintf("Query: %s\n\nContext:\n%s\n\nAnswer:", query, combinedCtx)

	payload := groqRequest{
		Model: "llama-3.1-8b-instant",
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.groqKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("groq error %d: %s", resp.StatusCode, string(raw))
	}

	var gr groqResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", err
	}
	if len(gr.Choices) == 0 {
		return "", fmt.Errorf("groq returned no choices")
	}
	return gr.Choices[0].Message.Content, nil
}

func (e *Engine) ensureCollection(ctx context.Context) error {
	_, err := e.collection.Get(ctx, &pb.GetCollectionInfoRequest{CollectionName: collectionName})
	if err == nil {
		return nil
	}

	_, err = e.collection.Create(ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: &pb.VectorsConfig{Config: &pb.VectorsConfig_Params{
			Params: &pb.VectorParams{
				Size:     vectorSize,
				Distance: pb.Distance_Cosine,
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("create qdrant collection: %w", err)
	}
	log.Printf("created Qdrant collection: %s", collectionName)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func strVal(s string) *pb.Value {
	return &pb.Value{Kind: &pb.Value_StringValue{StringValue: s}}
}

func getStr(payload map[string]*pb.Value, key string) string {
	if v, ok := payload[key]; ok {
		if s, ok := v.Kind.(*pb.Value_StringValue); ok {
			return s.StringValue
		}
	}
	return ""
}
