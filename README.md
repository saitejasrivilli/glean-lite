# glean-lite

A self-hosted, RAG-powered enterprise search demo built with Go, Qdrant, Groq, and Next.js.
Index your own GitHub repos and give recruiters a live URL to search your codebase.

## Architecture

```
Browser → Vercel (Next.js) → Fly.io (Go API) → Qdrant Cloud (vectors)
                                              → Groq (LLM synthesis)
                                              → HuggingFace (embeddings)
                                              → GitHub API (data source)
```

## Free Tier Stack

| Service       | Role              | Cost       |
|---------------|-------------------|------------|
| Fly.io        | Go backend        | Free       |
| Qdrant Cloud  | Vector DB         | Free (1GB) |
| Groq          | LLM inference     | Free       |
| HuggingFace   | Embeddings        | Free       |
| Vercel        | Next.js UI        | Free       |

## Local Setup

### 1. Clone and configure

```bash
git clone https://github.com/yourname/glean-lite
cd glean-lite
cp .env.example .env
# Fill in your API keys in .env
```

### 2. Start Qdrant locally (for dev)

```bash
docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant
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
cd ui
npm install
npm run dev
# Open http://localhost:3000
```

## Deployment

### Backend → Fly.io

```bash
# Install flyctl: https://fly.io/docs/hands-on/install-flyctl/
fly auth login
fly launch          # creates your app, detects Dockerfile
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

1. Sign up at cloud.qdrant.io (free tier)
2. Create a cluster → copy the URL and API key
3. Add to Fly.io secrets above

### UI → Vercel

```bash
# Install Vercel CLI
npm i -g vercel
cd ui
vercel          # follow prompts
# Set NEXT_PUBLIC_API_URL=https://your-app.fly.dev in Vercel dashboard
```

### Trigger indexing in production

After deployment, run once to index your repos:

```bash
curl -X POST https://your-app.fly.dev/api/index
```

Add this to a GitHub Actions workflow to re-index on every push.

## Adding a New Connector

1. Create `connectors/yourservice/yourservice.go`
2. Implement the `connector.Connector` interface (2 methods: `Name()` and `Fetch()`)
3. Register it in `internal/search/engine.go` in the `connectors` slice

## API

```
POST /api/search   { "query": "string" }  → { answer, sources[] }
POST /api/index    {}                      → { indexed: N }
GET  /api/health                           → { status: "ok" }
```
