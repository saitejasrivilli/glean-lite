"use client";

import { useState, useRef, useEffect, useCallback } from "react";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const HISTORY_KEY = "glean-lite-history";
const MAX_HISTORY = 5;

interface Source {
  id: string;
  title: string;
  url: string;
  snippet: string;
  score: number;
  metadata: Record<string, string>;
}

interface Stats {
  docs: number;
  repos: number;
  collection: string;
}

type State =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "streaming"; sources: Source[]; answer: string }
  | { status: "done"; sources: Source[]; answer: string }
  | { status: "error"; message: string };

const SUGGESTIONS = [
  "how does the RAG pipeline work",
  "explain the vector search implementation",
  "vLLM throughput benchmarking",
  "attention optimization techniques",
];

function getHistory(): string[] {
  if (typeof window === "undefined") return [];
  try {
    return JSON.parse(localStorage.getItem(HISTORY_KEY) ?? "[]");
  } catch {
    return [];
  }
}

function addToHistory(q: string) {
  if (typeof window === "undefined") return;
  const h = getHistory().filter((x) => x !== q);
  localStorage.setItem(HISTORY_KEY, JSON.stringify([q, ...h].slice(0, MAX_HISTORY)));
}

export default function Home() {
  const [query, setQuery] = useState("");
  const [state, setState] = useState<State>({ status: "idle" });
  const [stats, setStats] = useState<Stats | null>(null);
  const [history, setHistory] = useState<string[]>([]);
  const [hoveredSource, setHoveredSource] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    inputRef.current?.focus();
    setHistory(getHistory());
    fetch(`${API}/api/stats`)
      .then((r) => r.json())
      .then(setStats)
      .catch(() => {});

    // ⌘K to focus search
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        inputRef.current?.focus();
        inputRef.current?.select();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const search = useCallback(async (q: string) => {
    if (!q.trim()) return;
    if (abortRef.current) abortRef.current.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    addToHistory(q);
    setHistory(getHistory());
    setState({ status: "loading" });

    try {
      const res = await fetch(`${API}/api/search/stream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ query: q }),
        signal: controller.signal,
      });

      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);

      const reader = res.body!.getReader();
      const decoder = new TextDecoder();
      let sources: Source[] = [];
      let answer = "";
      let buf = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";

        for (const line of lines) {
          if (line.startsWith("event: ")) continue;
          if (!line.startsWith("data: ")) continue;
          const data = line.slice(6);
          if (!data.trim()) continue;

          try {
            const parsed = JSON.parse(data);
            if (Array.isArray(parsed)) {
              sources = parsed;
              setState({ status: "streaming", sources, answer: "" });
            } else if (parsed.token !== undefined) {
              answer += parsed.token;
              setState({ status: "streaming", sources, answer });
            } else if (parsed.message) {
              // error event
            }
          } catch {}
        }
      }

      setState({ status: "done", sources, answer });
    } catch (e: unknown) {
      if ((e as Error).name === "AbortError") return;
      setState({ status: "error", message: (e as Error).message ?? "Unknown error" });
    }
  }, []);

  function handleKey(e: React.KeyboardEvent) {
    if (e.key === "Enter") search(query);
  }

  const isActive = state.status !== "idle";

  return (
    <>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,300;0,9..40,400;0,9..40,500;1,9..40,300&family=DM+Mono:wght@400;500&display=swap');
        *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
        html, body { background: #F7F5F2; font-family: 'DM Sans', sans-serif; }
        ::selection { background: #C8B89A33; }
        .src-card:hover { border-color: #1A1A1A !important; }
        .chip:hover { border-color: #1A1A1A !important; color: #1A1A1A !important; }
        .new-search:hover { color: #1A1A1A !important; }
        @keyframes blink { 0%,100%{opacity:1} 50%{opacity:0} }
        .cursor { display:inline-block; width:2px; height:13px; background:#C8B89A; margin-left:1px; vertical-align:middle; animation: blink 1s step-end infinite; }
      `}</style>

      <div style={{ fontFamily: "'DM Sans', sans-serif", background: "#F7F5F2", minHeight: "100vh" }}>

        {/* Header */}
        <div style={{ background: "#1A1A1A", padding: "14px 32px", display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "13px", color: "#F7F5F2", letterSpacing: "0.08em", cursor: "pointer" }}
            onClick={() => { setState({ status: "idle" }); setQuery(""); }}>
            glean<span style={{ color: "#C8B89A" }}>-lite</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "16px" }}>
            {stats && (
              <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#555", letterSpacing: "0.04em" }}>
                {stats.docs.toLocaleString()} docs · {stats.repos} repos
              </div>
            )}
            <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#444", letterSpacing: "0.05em" }}>v0.1.0</div>
          </div>
        </div>

        {/* Body */}
        <div style={{ maxWidth: "720px", margin: "0 auto", padding: "48px 32px 80px" }}>

          {!isActive && (
            <>
              <div style={{ fontSize: "11px", fontWeight: 500, letterSpacing: "0.12em", textTransform: "uppercase", color: "#999", marginBottom: "10px" }}>
                Codebase search
              </div>
              <div style={{ fontSize: "30px", fontWeight: 300, color: "#1A1A1A", letterSpacing: "-0.02em", marginBottom: "32px", lineHeight: 1.2 }}>
                What are you looking for?
              </div>
            </>
          )}

          {/* Search bar */}
          <div style={{ background: "#fff", border: "1px solid #E0DDD8", borderRadius: "4px", display: "flex", alignItems: "center", padding: "12px 14px", gap: "10px", marginBottom: "10px", transition: "border-color 0.15s" }}>
            <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "13px", color: "#C8B89A", flexShrink: 0 }}>›</div>
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleKey}
              placeholder="ask anything about the codebase..."
              style={{ flex: 1, border: "none", outline: "none", fontFamily: "'DM Sans', sans-serif", fontSize: "14px", color: "#1A1A1A", background: "transparent" }}
            />
            <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "10px", color: "#CCC", flexShrink: 0, display: "flex", alignItems: "center", gap: "4px" }}>
              <kbd style={{ border: "1px solid #E0DDD8", borderRadius: "2px", padding: "1px 4px", fontSize: "10px" }}>⌘K</kbd>
            </div>
            <button
              onClick={() => search(query)}
              disabled={state.status === "loading" || state.status === "streaming"}
              style={{ background: "#1A1A1A", color: "#F7F5F2", border: "none", fontFamily: "'DM Sans', sans-serif", fontSize: "12px", fontWeight: 500, letterSpacing: "0.04em", padding: "8px 16px", borderRadius: "2px", cursor: "pointer", flexShrink: 0, opacity: (state.status === "loading" || state.status === "streaming") ? 0.5 : 1 }}
            >
              search
            </button>
          </div>

          {/* Suggestions + history */}
          {state.status === "idle" && (
            <div style={{ marginBottom: "48px" }}>
              {history.length > 0 && (
                <div style={{ marginBottom: "10px" }}>
                  <div style={{ fontSize: "10px", fontWeight: 500, letterSpacing: "0.1em", textTransform: "uppercase", color: "#BBB", marginBottom: "6px" }}>recent</div>
                  <div style={{ display: "flex", flexWrap: "wrap", gap: "6px" }}>
                    {history.map((h) => (
                      <button key={h} className="chip"
                        onClick={() => { setQuery(h); search(h); }}
                        style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#666", border: "1px solid #E0DDD8", background: "#FAF9F7", padding: "5px 10px", borderRadius: "2px", cursor: "pointer", transition: "border-color 0.1s, color 0.1s" }}>
                        ↺ {h}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              <div style={{ fontSize: "10px", fontWeight: 500, letterSpacing: "0.1em", textTransform: "uppercase", color: "#BBB", marginBottom: "6px" }}>suggested</div>
              <div style={{ display: "flex", flexWrap: "wrap", gap: "6px" }}>
                {SUGGESTIONS.map((s) => (
                  <button key={s} className="chip"
                    onClick={() => { setQuery(s); search(s); }}
                    style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#888", border: "1px solid #E0DDD8", background: "#fff", padding: "5px 10px", borderRadius: "2px", cursor: "pointer", transition: "border-color 0.1s, color 0.1s" }}>
                    {s}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Loading */}
          {state.status === "loading" && (
            <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "12px", color: "#999", padding: "32px 0" }}>
              embedding · searching · synthesizing...
            </div>
          )}

          {/* Error */}
          {state.status === "error" && (
            <div style={{ border: "1px solid #F0998A", background: "#FFF5F3", borderRadius: "4px", padding: "12px 16px", fontSize: "13px", color: "#993C1D", fontFamily: "'DM Mono', monospace", marginTop: "16px" }}>
              error: {state.message}
            </div>
          )}

          {/* Streaming / Done results */}
          {(state.status === "streaming" || state.status === "done") && (
            <div style={{ marginTop: "16px" }}>

              {/* Sources — show immediately */}
              {state.sources.length > 0 && (
                <div style={{ marginBottom: "24px" }}>
                  <div style={{ fontSize: "10px", fontWeight: 500, letterSpacing: "0.14em", textTransform: "uppercase", color: "#999", marginBottom: "10px", display: "flex", alignItems: "center", gap: "8px" }}>
                    sources ({state.sources.length})
                    <div style={{ flex: 1, height: "1px", background: "#E0DDD8" }} />
                  </div>
                  {state.sources.map((src, i) => (
                    <div key={src.id} style={{ position: "relative" }}>
                      <a
                        href={src.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="src-card"
                        onMouseEnter={() => setHoveredSource(src.id)}
                        onMouseLeave={() => setHoveredSource(null)}
                        style={{ background: "#fff", border: "1px solid #E0DDD8", borderRadius: "4px", padding: "12px 16px", marginBottom: "6px", display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: "12px", textDecoration: "none", transition: "border-color 0.12s" }}>
                        <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#C8B89A", flexShrink: 0, marginTop: "2px" }}>[{i + 1}]</div>
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ fontSize: "13px", fontWeight: 500, color: "#1A1A1A", marginBottom: "3px", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                            {src.title}
                          </div>
                          <div style={{ fontSize: "11px", color: "#888", fontFamily: "'DM Mono', monospace", whiteSpace: hoveredSource === src.id ? "normal" : "nowrap", overflow: hoveredSource === src.id ? "visible" : "hidden", textOverflow: hoveredSource === src.id ? "unset" : "ellipsis", lineHeight: 1.5 }}>
                            {hoveredSource === src.id ? src.snippet : src.snippet.slice(0, 100)}
                          </div>
                        </div>
                        <div style={{ textAlign: "right", flexShrink: 0 }}>
                          <div style={{ fontFamily: "'DM Mono', monospace", fontSize: "12px", color: "#1A1A1A", fontWeight: 500 }}>
                            {Math.round(src.score * 100)}%
                          </div>
                          {src.metadata.repo && (
                            <div style={{ fontSize: "10px", color: "#BBB", fontFamily: "'DM Mono', monospace" }}>
                              {src.metadata.repo.split("/")[1]}
                            </div>
                          )}
                        </div>
                      </a>
                    </div>
                  ))}
                </div>
              )}

              {/* Answer — streams in */}
              {(state.answer || state.status === "streaming") && (
                <div style={{ marginBottom: "32px" }}>
                  <div style={{ fontSize: "10px", fontWeight: 500, letterSpacing: "0.14em", textTransform: "uppercase", color: "#C8B89A", marginBottom: "10px", display: "flex", alignItems: "center", gap: "8px" }}>
                    answer
                    <div style={{ flex: 1, height: "1px", background: "#E0DDD8" }} />
                  </div>
                  <div style={{ fontSize: "14px", lineHeight: 1.8, color: "#333", background: "#fff", border: "1px solid #E0DDD8", borderLeft: "3px solid #C8B89A", padding: "16px 20px", borderRadius: "0 4px 4px 0", whiteSpace: "pre-wrap" }}>
                    {state.answer}
                    {state.status === "streaming" && <span className="cursor" />}
                  </div>
                </div>
              )}

              <button
                className="new-search"
                onClick={() => { setState({ status: "idle" }); setQuery(""); }}
                style={{ fontFamily: "'DM Mono', monospace", fontSize: "11px", color: "#BBB", background: "none", border: "none", cursor: "pointer", transition: "color 0.1s" }}>
                ← new search
              </button>
            </div>
          )}

          {/* Footer */}
          <div style={{ marginTop: "64px", paddingTop: "20px", borderTop: "1px solid #E0DDD8", display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
            <div style={{ fontSize: "10px", color: "#CCC", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 500, marginRight: "4px" }}>stack</div>
            {["Go", "Qdrant", "Groq", "HuggingFace", "Next.js"].map(t => (
              <div key={t} style={{ fontFamily: "'DM Mono', monospace", fontSize: "10px", color: "#888", border: "1px solid #E0DDD8", padding: "3px 8px", borderRadius: "2px" }}>{t}</div>
            ))}
            <div style={{ flex: 1 }} />
            <a href="https://github.com/saitejasrivilli/glean-lite" target="_blank" rel="noopener noreferrer"
              style={{ fontFamily: "'DM Mono', monospace", fontSize: "10px", color: "#BBB", textDecoration: "none" }}>
              github ↗
            </a>
          </div>

        </div>
      </div>
    </>
  );
}
