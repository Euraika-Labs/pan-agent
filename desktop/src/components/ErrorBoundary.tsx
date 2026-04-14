import React from "react";

// Props accepted by ErrorBoundary. `screenName` is used in the fallback
// message so the user knows which screen crashed; `onReset` is called when
// the user clicks "Try again" so the parent can reload data or switch
// screens.
interface Props {
  children: React.ReactNode;
  screenName?: string;
  onReset?: () => void;
}

interface State {
  error: Error | null;
}

/**
 * ErrorBoundary catches render-time exceptions from a single screen and
 * shows a humane fallback instead of white-screening the whole app.
 *
 * Why: before Phase 11's audit pass, one uncaught error in (say) Memory.tsx
 * reading `data.stats.totalSessions` off an undefined `data` blanked the
 * entire layout — the user couldn't even switch away from the broken
 * screen. React class components are the only way to `componentDidCatch`;
 * hooks don't support it yet (React 19 still lacks useErrorBoundary).
 */
export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    // Log to console for dev. In production we could ship this to a
    // telemetry endpoint if one exists; none is wired up yet.
    console.error("[ErrorBoundary]", this.props.screenName, error, info);
  }

  handleReset = (): void => {
    this.setState({ error: null });
    this.props.onReset?.();
  };

  render(): React.ReactNode {
    if (this.state.error) {
      const name = this.props.screenName ?? "this screen";
      return (
        <div
          style={{
            padding: "32px",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: "12px",
            color: "var(--text-secondary, #666)",
            textAlign: "center",
          }}
        >
          <div style={{ fontSize: "1.1rem", fontWeight: 500 }}>
            Something went wrong in {name}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "0.85rem",
              background: "var(--bg-tertiary, #f4f4f4)",
              padding: "8px 12px",
              borderRadius: "6px",
              maxWidth: "600px",
              wordBreak: "break-word",
            }}
          >
            {this.state.error.message}
          </div>
          <button
            onClick={this.handleReset}
            className="btn btn-secondary"
            style={{ marginTop: "4px" }}
          >
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

export default ErrorBoundary;
