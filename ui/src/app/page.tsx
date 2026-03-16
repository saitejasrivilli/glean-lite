"use client";

import { useState, useRef, useEffect } from "react";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

interface Source {
  id: string;
  title: string;
  url: string;
  snippet: string;
  score: number;
  metadata: Record<string, string>;
}

interface SearchResponse {
  answer: string;
  sources: Source[];
}

type State =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "done"; data: SearchResponse }
  | { status: "error"; message: string };

const SUGGESTIONS = [
  "How does the RAG pipeline work?",
  "What connectors are available?",
  "Explain the vector search implementation",
  "How is the Qdrant collection structured?",
];

export default function Home() {
  const [query, setQuery] = useState("");
  const [state, setState] = useState<State>({ status: "idle" });
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  async function search(q: string) {
    if (!q.trim()) return;
    setState({ status: "loading" });
    try {
      const res = await fetch(`${API}/api/search`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ query: q }),
      });
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
      const data: SearchResponse = await res.json();
      setState({ status: "done", data });
    } catch (e: unknown) {
      setState({ status: "error", message: e instanceof Error ? e.message : "Unknown error" });
    }
  }

  function handleKey(e: React.KeyboardEvent) {
    if (e.key === "Enter") search(query);
  }

  return (
    <main className="min-h-screen bg-[#0a0a0f] text-[#e2e8f0] font-mono">
      {/* Subtle grid bg */}
      <div
        className="fixed inset-0 pointer-events-none"
        style={{
          backgroundImage:
            "linear-gradient(rgba(99,102,241,.04) 1px, transparent 1px), linear-gradient(90deg, rgba(99,102,241,.04) 1px, transparent 1px)",
          backgroundSize: "40px 40px",
        }}
      />

      <div className="relative max-w-3xl mx-auto px-4 py-20">
        {/* Header */}
        <div className="mb-12">
          <div className="flex items-center gap-3 mb-2">
            <span className="text-indigo-400 text-xs tracking-[0.3em] uppercase">
              ▸ glean-lite
            </span>
            <span className="text-[#2d2d3d] text-xs">v0.1.0</span>
          </div>
          <h1 className="text-4xl font-bold tracking-tight text-white mb-2">
            Search your codebase
          </h1>
          <p className="text-[#64748b] text-sm">
            RAG-powered search across GitHub repos · Qdrant + Groq + HuggingFace
          </p>
        </div>

        {/* Search bar */}
        <div className="relative mb-4">
          <div className="absolute left-4 top-1/2 -translate-y-1/2 text-indigo-400 text-sm select-none">
            ❯
          </div>
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKey}
            placeholder="Ask anything about the codebase..."
            className="w-full bg-[#0f0f1a] border border-[#1e1e2e] rounded-lg pl-10 pr-32 py-4 text-sm text-white placeholder-[#3d3d5c] focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
          />
          <button
            onClick={() => search(query)}
            disabled={state.status === "loading"}
            className="absolute right-2 top-1/2 -translate-y-1/2 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 text-white text-xs px-4 py-2 rounded-md transition-colors"
          >
            {state.status === "loading" ? "searching..." : "search →"}
          </button>
        </div>

        {/* Suggestion pills */}
        {state.status === "idle" && (
          <div className="flex flex-wrap gap-2 mb-12">
            {SUGGESTIONS.map((s) => (
              <button
                key={s}
                onClick={() => { setQuery(s); search(s); }}
                className="text-xs text-[#64748b] border border-[#1e1e2e] rounded-full px-3 py-1 hover:border-indigo-500/40 hover:text-indigo-300 transition-all"
              >
                {s}
              </button>
            ))}
          </div>
        )}

        {/* Loading */}
        {state.status === "loading" && (
          <div className="text-[#64748b] text-sm animate-pulse py-8">
            ⠋ embedding query · searching vectors · synthesizing answer...
          </div>
        )}

        {/* Error */}
        {state.status === "error" && (
          <div className="border border-red-900/50 bg-red-950/20 rounded-lg p-4 text-red-400 text-sm">
            ✗ {state.message}
          </div>
        )}

        {/* Results */}
        {state.status === "done" && (
          <div className="space-y-6">
            {/* AI Answer */}
            <div className="border border-indigo-500/20 bg-indigo-950/20 rounded-lg p-5">
              <div className="text-xs text-indigo-400 mb-3 tracking-widest uppercase">
                ✦ answer
              </div>
              <p className="text-sm text-[#cbd5e1] leading-relaxed whitespace-pre-wrap">
                {state.data.answer}
              </p>
            </div>

            {/* Sources */}
            <div>
              <div className="text-xs text-[#475569] mb-3 tracking-widest uppercase">
                ↗ sources ({state.data.sources.length})
              </div>
              <div className="space-y-2">
                {state.data.sources.map((src, i) => (
                  <a
                    key={src.id}
                    href={src.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block border border-[#1e1e2e] hover:border-[#2d2d3d] bg-[#0d0d18] hover:bg-[#0f0f1c] rounded-lg p-4 transition-all group"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-[#3d3d5c] text-xs">[{i + 1}]</span>
                          <span className="text-indigo-300 text-sm truncate group-hover:text-indigo-200 transition-colors">
                            {src.title}
                          </span>
                        </div>
                        <p className="text-xs text-[#475569] line-clamp-2">{src.snippet}</p>
                      </div>
                      <div className="shrink-0 text-right">
                        <span className="text-xs text-[#2d2d3d] font-mono">
                          {(src.score * 100).toFixed(0)}%
                        </span>
                        {src.metadata.repo && (
                          <div className="text-[10px] text-[#2d2d3d] mt-1">{src.metadata.repo}</div>
                        )}
                      </div>
                    </div>
                  </a>
                ))}
              </div>
            </div>

            {/* New search */}
            <button
              onClick={() => setState({ status: "idle" })}
              className="text-xs text-[#3d3d5c] hover:text-[#64748b] transition-colors"
            >
              ← new search
            </button>
          </div>
        )}
      </div>
    </main>
  );
}
