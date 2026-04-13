import { useState, useRef, useCallback } from "react";
import { Search as SearchIcon, X } from "lucide-react";
import { fetchJSON } from "../../api";

interface SearchResult {
  sessionId: string;
  title: string | null;
  startedAt: number;
  source: string;
  messageCount: number;
  model: string;
  snippet: string;
}

interface SearchProps {
  onResumeSession: (sessionId: string) => void;
  currentSessionId: string | null;
}

function formatFullDate(ts: number): string {
  const date = new Date(ts * 1000);
  return (
    date.toLocaleDateString([], { month: "short", day: "numeric" }) +
    ", " +
    date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  );
}

function formatModel(model: string): string {
  const name = model.split("/").pop() || model;
  return name.split(":")[0];
}

function highlightSnippet(snippet: string): React.JSX.Element {
  const parts = snippet.split(/(<<.*?>>)/g);
  return (
    <span>
      {parts.map((part, i) => {
        if (part.startsWith("<<") && part.endsWith(">>")) {
          return <mark key={i}>{part.slice(2, -2)}</mark>;
        }
        return <span key={i}>{part}</span>;
      })}
    </span>
  );
}

function Search({ onResumeSession, currentSessionId }: SearchProps): React.JSX.Element {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const runSearch = useCallback(async (q: string): Promise<void> => {
    if (!q.trim()) {
      setResults([]);
      setHasSearched(false);
      return;
    }
    setSearching(true);
    try {
      const data = await fetchJSON<SearchResult[]>(
        `/v1/sessions?q=${encodeURIComponent(q)}`,
      );
      setResults(data);
      setHasSearched(true);
    } catch (err) {
      console.error("[Search] error:", err);
      setResults([]);
    } finally {
      setSearching(false);
    }
  }, []);

  function handleChange(value: string): void {
    setQuery(value);
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      runSearch(value);
    }, 300);
  }

  function handleClear(): void {
    setQuery("");
    setResults([]);
    setHasSearched(false);
    inputRef.current?.focus();
  }

  return (
    <div className="sessions-container">
      <div className="sessions-header">
        <div className="sessions-header-top">
          <h2 className="sessions-title">Search</h2>
        </div>
        <div className="sessions-searchbar">
          <SearchIcon size={14} className="sessions-searchbar-icon" />
          <input
            ref={inputRef}
            className="sessions-searchbar-input"
            type="text"
            placeholder="Search all conversations..."
            value={query}
            onChange={(e) => handleChange(e.target.value)}
            autoFocus
          />
          {query && (
            <button className="btn-ghost sessions-searchbar-clear" onClick={handleClear}>
              <X size={13} />
            </button>
          )}
        </div>
      </div>

      {searching ? (
        <div className="sessions-loading">
          <div className="loading-spinner" />
        </div>
      ) : hasSearched && results.length === 0 ? (
        <div className="sessions-empty">
          <SearchIcon size={32} className="sessions-empty-icon" />
          <p className="sessions-empty-text">No results found</p>
          <p className="sessions-empty-hint">Try different search terms</p>
        </div>
      ) : (
        <div className="sessions-list">
          {results.map((r) => (
            <button
              key={r.sessionId}
              className={`sessions-card ${currentSessionId === r.sessionId ? "sessions-card--active" : ""}`}
              onClick={() => onResumeSession(r.sessionId)}
            >
              <div className="sessions-card-main">
                <span className="sessions-card-title">
                  {r.title || `Session ${r.sessionId.slice(-6)}`}
                </span>
                <span className="sessions-card-time">
                  {formatFullDate(r.startedAt)}
                </span>
              </div>
              {r.snippet && (
                <div className="sessions-result-snippet">
                  {highlightSnippet(r.snippet)}
                </div>
              )}
              <div className="sessions-card-tags">
                <span className="sessions-tag sessions-tag--source">
                  {r.source}
                </span>
                <span className="sessions-tag">
                  {r.messageCount} msg{r.messageCount !== 1 ? "s" : ""}
                </span>
                {r.model && (
                  <span className="sessions-tag sessions-tag--model">
                    {formatModel(r.model)}
                  </span>
                )}
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export default Search;
