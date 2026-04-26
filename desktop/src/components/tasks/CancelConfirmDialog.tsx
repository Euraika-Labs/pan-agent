import { useEffect, useRef } from "react";
import type { Task } from "../../api";

interface CancelConfirmDialogProps {
  task: Task | null;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Focus-trapped modal asking the user to confirm cancelling a task.
 * Mirrors the WS#2 `<UndoConfirmDialog>` pattern (Esc + backdrop close,
 * auto-focus primary, aria-modal). Cancel is one-click on the row card
 * today; the dialog gates against accidental clicks now that running
 * tasks may have already produced reversible journal receipts.
 */
export function CancelConfirmDialog({
  task,
  onConfirm,
  onCancel,
}: CancelConfirmDialogProps): React.JSX.Element | null {
  const primaryRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!task) return;
    primaryRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onCancel();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [task, onCancel]);

  if (!task) return null;

  const shortId = task.id.slice(-8);

  return (
    <div
      className="cancel-dialog-backdrop"
      onClick={onCancel}
      role="presentation"
    >
      <div
        className="cancel-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="cancel-dialog-title"
        aria-describedby="cancel-dialog-desc"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="cancel-dialog-title" className="cancel-dialog-title">
          Cancel task-{shortId}?
        </h2>
        <p id="cancel-dialog-desc" className="cancel-dialog-desc">
          The task will stop after the current step. Any actions it has
          already taken stay in the journal — you can review or undo
          individual receipts in the History screen, but cancellation
          itself does not roll back state.
        </p>
        <div className="cancel-dialog-actions">
          <button
            ref={primaryRef}
            type="button"
            className="cancel-dialog-btn cancel-dialog-btn--primary"
            onClick={onConfirm}
          >
            Cancel task
          </button>
          <button
            type="button"
            className="cancel-dialog-btn"
            onClick={onCancel}
          >
            Keep running
          </button>
        </div>
      </div>
    </div>
  );
}
