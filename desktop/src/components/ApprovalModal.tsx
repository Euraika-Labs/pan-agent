import React, { useEffect, useRef, useState } from "react";
import { AlertTriangle, OctagonAlert, Eye, X } from "lucide-react";

export interface ApprovalRequest {
  id: string;
  level: 1 | 2;
  command: string;
  description: string;
  patternKey: string;
  reason: string;
  previewAvailable?: boolean;
}

export type ApprovalResponse = "approved" | "denied" | "preview";

interface ApprovalModalProps {
  request: ApprovalRequest | null;
  onResponse: (id: string, response: ApprovalResponse) => void;
}

const CONFIRMATION_PHRASE = "YES-I-UNDERSTAND-THE-RISK";

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function ApprovalModal({
  request,
  onResponse,
}: ApprovalModalProps): React.JSX.Element | null {
  const [phrase, setPhrase] = useState("");
  const containerRef = useRef<HTMLDivElement | null>(null);
  const cancelButtonRef = useRef<HTMLButtonElement | null>(null);
  const previouslyFocusedRef = useRef<Element | null>(null);

  useEffect(() => {
    if (!request) return;
    setPhrase("");
  }, [request]);

  useEffect(() => {
    if (!request) return;

    previouslyFocusedRef.current = document.activeElement;
    const focusTimer = window.setTimeout(() => {
      cancelButtonRef.current?.focus();
    }, 0);

    return () => {
      window.clearTimeout(focusTimer);
      const prev = previouslyFocusedRef.current;
      if (prev instanceof HTMLElement) {
        prev.focus();
      }
    };
  }, [request]);

  useEffect(() => {
    if (!request) return;

    const handleKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        if (request.level === 1) {
          event.preventDefault();
          onResponse(request.id, "denied");
        } else {
          event.preventDefault();
        }
        return;
      }

      if (event.key === "Tab") {
        const container = containerRef.current;
        if (!container) return;
        const focusables = Array.from(
          container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
        ).filter((el) => !el.hasAttribute("aria-hidden"));
        if (focusables.length === 0) {
          event.preventDefault();
          return;
        }
        const first = focusables[0];
        const last = focusables[focusables.length - 1];
        const active = document.activeElement as HTMLElement | null;

        if (event.shiftKey) {
          if (active === first || !container.contains(active)) {
            event.preventDefault();
            last.focus();
          }
        } else {
          if (active === last) {
            event.preventDefault();
            first.focus();
          }
        }
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [request, onResponse]);

  if (!request) return null;

  const isLevel2 = request.level === 2;
  const phraseMatches = phrase.trim() === CONFIRMATION_PHRASE;
  const confirmDisabled = isLevel2 && !phraseMatches;

  const titleId = `approval-modal-title-${request.id}`;
  const descriptionId = `approval-modal-desc-${request.id}`;

  const handleConfirm = (): void => {
    if (confirmDisabled) return;
    onResponse(request.id, "approved");
  };

  const handleCancel = (): void => {
    onResponse(request.id, "denied");
  };

  const handlePreview = (): void => {
    onResponse(request.id, "preview");
  };

  return (
    <div
      className="approval-modal-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !isLevel2) {
          handleCancel();
        }
      }}
    >
      <div
        ref={containerRef}
        className={`approval-modal ${isLevel2 ? "approval-level-2" : "approval-level-1"}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
      >
        <div className="approval-modal-header">
          <div className="approval-modal-header-icon" aria-hidden="true">
            {isLevel2 ? (
              <OctagonAlert size={22} />
            ) : (
              <AlertTriangle size={22} />
            )}
          </div>
          <h2 id={titleId} className="approval-modal-title">
            {isLevel2 ? "STOP — Irreversible action" : "Approval required"}
          </h2>
          {!isLevel2 && (
            <button
              type="button"
              className="approval-modal-close"
              aria-label="Close"
              onClick={handleCancel}
            >
              <X size={16} />
            </button>
          )}
        </div>

        <div className="approval-modal-body">
          <p id={descriptionId} className="approval-modal-description">
            {request.description}
          </p>

          <pre className="approval-modal-command">
            <code>{request.command}</code>
          </pre>

          <div className="approval-modal-meta">
            <span className="approval-modal-pattern-tag">
              {request.patternKey}
            </span>
          </div>

          {isLevel2 && (
            <div className="approval-modal-reason" role="note">
              <strong>Why this is dangerous:</strong> {request.reason}
            </div>
          )}

          {isLevel2 && (
            <div className="approval-modal-phrase-field">
              <label
                htmlFor={`approval-phrase-${request.id}`}
                className="approval-modal-phrase-label"
              >
                Type this exact phrase to proceed:{" "}
                <code>{CONFIRMATION_PHRASE}</code>
              </label>
              <input
                id={`approval-phrase-${request.id}`}
                type="text"
                className="approval-modal-phrase-input"
                value={phrase}
                onChange={(e) => setPhrase(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                aria-invalid={phrase.length > 0 && !phraseMatches}
              />
            </div>
          )}
        </div>

        <div className="approval-modal-actions">
          {request.previewAvailable && !isLevel2 && (
            <button
              type="button"
              className="approval-modal-btn-preview"
              onClick={handlePreview}
            >
              <Eye size={14} />
              Preview
            </button>
          )}
          <button
            type="button"
            ref={cancelButtonRef}
            className="approval-modal-btn-cancel"
            onClick={handleCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="approval-modal-btn-confirm"
            onClick={handleConfirm}
            disabled={confirmDisabled}
          >
            {isLevel2 ? "Proceed" : "Run this command"}
          </button>
        </div>
      </div>
    </div>
  );
}

export default ApprovalModal;
