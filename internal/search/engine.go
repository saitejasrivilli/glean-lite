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
	"sync/atomic"

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
	docCount   atomic.Int64
	repoCount  int
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

	repos := strings.Split(os.Getenv("GITHUB_REPOS"), ",")
	repoCount := 0
	for _, r := range repos {
		if strings.TrimSpace(r) != "" {
			repoCount++
		}
	}

	e := &Engine{
		qdrant:     pb.NewPointsClient(conn),
		collection: pb.NewCollectionsClient(conn),
		embedder:   embed.NewClient(),
		groqKey:    os.Getenv("GROQ_API_KEY"),
		qdrantKey:  qdrantKey,
		repoCount:  repoCount,
		connectors: []connector.Connector{
			github.New(),
		},
	}

	if err := e.ensureCollection(e.authCtx(context.Background())); err != nil {
		return nil, err
	}

	// load existing doc count from Qdrant
	go e.refreshDocCount()

	return e, nil
}

func (e *Engine) authCtx(ctx context.Context) context.Context {
	if e.qdrantKey == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "api-key", e.qdrantKey)
}

func (e *Engine) refreshDocCount() {
	ctx := e.authCtx(context.Background())
	info, err := e.collection.Get(ctx, &pb.GetCollectionInfoRequest{CollectionName: collectionName})
	if err != nil {
		return
	}
	if info.Result != nil && info.Result.PointsCount != nil {
		e.docCount.Store(int64(*info.Result.PointsCount))
	}
}

func (e *Engine) Router() chi.Router {
	r := chi.NewRouter()
	r.Post("/search", e.HandleSearch)
	r.Post("/search/stream", e.HandleSearchStream)
	r.Post("/index", e.HandleIndex)
	r.Get("/stats", e.HandleStats)
	return r
}

// ---- Stats ----

type StatsResponse struct {
	Docs       int64  `json:"docs"`
	Repos      int    `json:"repos"`
	Collection string `json:"collection"`
}

func (e *Engine) HandleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StatsResponse{
		Docs:       e.docCount.Load(),
		Repos:      e.repoCount,
		Collection: collectionName,
	})
}

// ---- Search (non-streaming) ----

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

// ---- Streaming search (SSE) ----

func (e *Engine) HandleSearchStream(w http.ResponseWriter, r *http.Request) {
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

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sendEvent := func(eventType, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
		flusher.Flush()
	}

	// 1. Embed
	vec, err := e.embedder.Embed(req.Query)
	if err != nil {
		sendEvent("error", `{"message":"embedding failed"}`)
		return
	}

	// 2. Vector search
	results, err := e.vectorSearch(ctx, vec)
	if err != nil {
		sendEvent("error", `{"message":"search failed"}`)
		return
	}

	// Send sources immediately
	sourcesJSON, _ := json.Marshal(results)
	sendEvent("sources", string(sourcesJSON))

	// 3. Stream Groq answer
	if err := e.streamAnswer(ctx, req.Query, results, func(chunk string) {
		chunkJSON, _ := json.Marshal(map[string]string{"token": chunk})
		sendEvent("token", string(chunkJSON))
	}); err != nil {
		log.Printf("groq stream error: %v", err)
		sendEvent("token", `{"token":" (generation failed)"}`)
	}

	sendEvent("done", `{}`)
}

// ---- Index ----

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

	e.docCount.Store(int64(total))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"indexed": total})
}

// ---- Core Logic ----

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

// ---- Groq LLM ----

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
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

func buildMessages(query string, results []SearchResult) []groqMessage {
	var ctxParts []string
	for i, r := range results {
		ctxParts = append(ctxParts, fmt.Sprintf("[%d] %s\n%s\nSource: %s", i+1, r.Title, r.Snippet, r.URL))
	}
	combinedCtx := strings.Join(ctxParts, "\n\n---\n\n")

	systemPrompt := `You are glean-lite, an intelligent codebase search assistant.
Given context chunks from a developer's GitHub repos, answer concisely and accurately.
Cite sources using [1], [2] etc. If context is insufficient, say so honestly.
Keep answers under 150 words.`

	return []groqMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Query: %s\n\nContext:\n%s\n\nAnswer:", query, combinedCtx)},
	}
}

func (e *Engine) generateAnswer(ctx context.Context, query string, results []SearchResult) (string, error) {
	if e.groqKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY not set")
	}

	payload := groqRequest{
		Model:    "llama-3.1-8b-instant",
		Messages: buildMessages(query, results),
	}

	body, _ := json.Marshal(payload)
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

// streamAnswer calls Groq with stream:true and invokes onChunk for each token.
func (e *Engine) streamAnswer(ctx context.Context, query string, results []SearchResult, onChunk func(string)) error {
	if e.groqKey == "" {
		return fmt.Errorf("GROQ_API_KEY not set")
	}

	payload := groqRequest{
		Model:    "llama-3.1-8b-instant",
		Messages: buildMessages(query, results),
		Stream:   true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.groqKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("groq stream error %d: %s", resp.StatusCode, string(raw))
	}

	buf := make([]byte, 4096)
	var leftover string
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			lines := strings.Split(leftover+string(buf[:n]), "\n")
			leftover = ""
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					return nil
				}
				var chunk struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr != nil {
					continue
				}
				if len(chunk.Choices) > 0 {
					token := chunk.Choices[0].Delta.Content
					if token != "" {
						onChunk(token)
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- Qdrant Setup ----

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

// ---- Helpers ----

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
