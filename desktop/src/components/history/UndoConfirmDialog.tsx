import { useEffect, useRef } from "react";
import type { ReceiptDTO } from "../../api";

interface UndoConfirmDialogProps {
  receipt: ReceiptDTO | null;
  onConfirm: () => void;
  onCancel: () => void;
}

const KIND_LABEL: Record<ReceiptDTO["kind"], string> = {
  fs_write: "File write",
  fs_delete: "File delete",
  shell: "Shell command",
  browser_form: "Browser form",
  saas_api: "SaaS API call",
};

const KIND_HINT: Record<ReceiptDTO["kind"], string> = {
  fs_write:
    "The file will be restored from the pre-mutation snapshot. Any edits made after this action will be overwritten.",
  fs_delete:
    "The deleted file will be restored from the pre-mutation snapshot.",
  shell:
    "Reversing a shell action requires approval. After confirming, you'll be prompted to approve the inverse command.",
  browser_form:
    "Browser-form actions cannot be auto-reversed. Confirming will mark the receipt as needing manual reversal in the target service.",
  saas_api:
    "SaaS API actions cannot be locally reversed. Open the deep link to undo in the service's own UI.",
};

/**
 * Confirmation modal shown before triggering a reversal. Focus-trapped,
 * Esc / backdrop-click both cancel. Mirrors the BudgetExceededDialog
 * pattern (auto-focus primary, aria-modal, no portal lib needed).
 */
export function UndoConfirmDialog({
  receipt,
  onConfirm,
  onCancel,
}: UndoConfirmDialogProps): React.JSX.Element | null {
  const primaryRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!receipt) return;
    primaryRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onCancel();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [receipt, onCancel]);

  if (!receipt) return null;

  const kindLabel = KIND_LABEL[receipt.kind];
  const hint = KIND_HINT[receipt.kind];

  return (
    <div
      className="undo-dialog-backdrop"
      onClick={onCancel}
      role="presentation"
    >
      <div
        className="undo-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="undo-dialog-title"
        aria-describedby="undo-dialog-desc"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="undo-dialog-title" className="undo-dialog-title">
          Undo {kindLabel.toLowerCase()}?
        </h2>
        <p id="undo-dialog-desc" className="undo-dialog-desc">
          {hint}
        </p>
        {receipt.redactedPayload && (
          <pre
            className="undo-dialog-payload"
            aria-label="Redacted payload preview"
          >
            {receipt.redactedPayload.slice(0, 600)}
            {receipt.redactedPayload.length > 600 ? "\n…" : ""}
          </pre>
        )}
        <div className="undo-dialog-actions">
          <button
            ref={primaryRef}
            type="button"
            className="undo-dialog-btn undo-dialog-btn--primary"
            onClick={onConfirm}
          >
            Undo
          </button>
          <button
            type="button"
            className="undo-dialog-btn"
            onClick={onCancel}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
