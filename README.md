# glean-lite

A self-hosted, RAG-powered enterprise search engine that indexes your GitHub repos and answers questions about your code — with citations back to the source files.

**Live demo → [glean-lite.vercel.app](https://glean-lite.vercel.app)**

> Search "how does the RAG pipeline work?" and watch it pull answers from real code across 10 repos in under a second.

---

## What it does

Type a question. Get a cited answer synthesized from your actual codebase, with source files ranked by semantic relevance. Like Glean or Notion AI, but built from scratch and running on your own infrastructure.

- **Semantic search** — not keyword matching. Understands intent.
- **Streaming answers** — tokens appear in real time via SSE, sources load instantly
- **Source citations** — every answer links back to the exact GitHub file
- **⌘K** to focus search from anywhere
- **Search history** — last 5 queries saved locally
- **Live stats** — docs indexed and repos tracked in the header

---

## Architecture

```
Browser (Vercel) → Go API (Fly.io) → Qdrant Cloud (vectors)
                                   → Groq (LLM, streaming)
                                   → HuggingFace (embeddings)
                                   → GitHub API (data source)
```

### Request flow

```
1. User types query
2. HuggingFace embeds query → 384-dim vector
3. Qdrant cosine similarity search → top 5 chunks
4. Sources streamed to browser immediately (SSE)
5. Groq llama-3.1-8b-instant synthesizes cited answer
6. Answer tokens stream to browser in real time
```

---

## Free tier stack

| Service | Role | Cost |
|---|---|---|
| Fly.io | Go backend | Free |
| Qdrant Cloud | Vector DB (1GB) | Free |
| Groq | LLM inference | Free |
| HuggingFace | Embeddings | Free |
| Vercel | Next.js UI | Free |

**Total monthly cost: $0**

---

## API

```
POST /api/search         { "query": "string" } → { answer, sources[] }
POST /api/search/stream  { "query": "string" } → SSE stream (sources + tokens)
POST /api/index          {}                     → { indexed: N }
GET  /api/stats                                 → { docs, repos, collection }
GET  /api/health                                → { status: "ok" }
```

---

## Local setup

### Prerequisites

- Go 1.22+
- Node.js 18+
- Docker (for local Qdrant)

### 1. Clone and configure

```bash
git clone https://github.com/saitejasrivilli/glean-lite
cd glean-lite
cp .env.example .env
# Fill in your API keys
```

### 2. Start Qdrant locally

```bash
docker run -d -p 6333:6333 -p 6334:6334 --name qdrant qdrant/qdrant
```

### 3. Run the backend

```bash
go run ./cmd/server
```

### 4. Index your repos

```bash
curl -X POST http://localhost:8080/api/index
```

### 5. Run the UI

```bash
cd ui && npm install && npm run dev
# Open http://localhost:3000
```

---

## Deployment

### Backend → Fly.io

```bash
fly auth login
fly launch --name glean-lite --region iad --no-deploy
fly secrets set \
  QDRANT_ADDR=your-cluster.cloud.qdrant.io:6334 \
  QDRANT_API_KEY=... \
  GROQ_API_KEY=... \
  HF_API_KEY=... \
  GITHUB_TOKEN=... \
  GITHUB_REPOS=yourname/repo1,yourname/repo2
fly deploy
```

### Vector DB → Qdrant Cloud

Sign up at [cloud.qdrant.io](https://cloud.qdrant.io) → free cluster → copy endpoint + API key.

### UI → Vercel

```bash
cd ui && npx vercel
# Set NEXT_PUBLIC_API_URL=https://your-app.fly.dev in Vercel dashboard
```

### Trigger initial index

```bash
curl -X POST https://your-app.fly.dev/api/index
```

---

## Adding a connector

Every data source implements one interface:

```go
type Connector interface {
    Name() string
    Fetch(ctx context.Context) ([]Document, error)
}
```

Add a new file under `connectors/`, implement those two methods, register it in `internal/search/engine.go`. Done. Confluence, Notion, Slack — same pattern.

---

## CI/CD

Every push to `main` automatically:
1. Deploys the Go backend to Fly.io
2. Reindexes all configured repos

Configured via `.github/workflows/`.

---

## Tech decisions

**Why Go?** Fast compilation, single binary deploy, excellent concurrency for streaming HTTP. The whole backend is ~600 lines.

**Why Qdrant over Pinecone/Weaviate?** Free 1GB tier, gRPC client, runs locally via Docker for development parity with production.

**Why Groq over OpenAI?** 500+ tokens/sec on free tier — streaming feels instant rather than slow. Critical for the demo experience.

**Why HuggingFace embeddings?** `all-MiniLM-L6-v2` is 384-dim (small = fast), well-tested for code search, and free on the inference API.

---

## Author

**Sai Teja Srivilli** — [github.com/saitejasrivilli](https://github.com/saitejasrivilli)

Built as a portfolio project targeting enterprise search roles (Glean, Notion, Atlassian).
