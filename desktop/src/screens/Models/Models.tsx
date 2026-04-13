import { useState, useEffect, useCallback } from "react";
import { Plus, Trash2 as Trash, Search, X } from "lucide-react";
import { fetchJSON } from "../../api";
import { PROVIDERS } from "../../constants";

interface SavedModel {
  id: string;
  name: string;
  provider: string;
  model: string;
  baseUrl: string;
  createdAt: number;
}

function providerLabel(value: string): string {
  return PROVIDERS.options.find((p) => p.value === value)?.label || value;
}

function Models(): React.JSX.Element {
  const [models, setModels] = useState<SavedModel[]>([]);
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Modal state
  const [showModal, setShowModal] = useState(false);
  const [editingModel, setEditingModel] = useState<SavedModel | null>(null);
  const [formName, setFormName] = useState("");
  const [formProvider, setFormProvider] = useState("openrouter");
  const [formModel, setFormModel] = useState("");
  const [formBaseUrl, setFormBaseUrl] = useState("");
  const [formError, setFormError] = useState("");

  const loadModels = useCallback(async () => {
    try {
      const list = await fetchJSON<SavedModel[]>("/v1/models");
      setModels(list);
    } catch (err) {
      console.error("[Models] load error:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadModels();
  }, [loadModels]);

  function openAddModal(): void {
    setEditingModel(null);
    setFormName("");
    setFormProvider("openrouter");
    setFormModel("");
    setFormBaseUrl("");
    setFormError("");
    setShowModal(true);
  }

  function openEditModal(m: SavedModel): void {
    setEditingModel(m);
    setFormName(m.name);
    setFormProvider(m.provider);
    setFormModel(m.model);
    setFormBaseUrl(m.baseUrl);
    setFormError("");
    setShowModal(true);
  }

  function closeModal(): void {
    setShowModal(false);
    setEditingModel(null);
    setFormError("");
  }

  async function handleSave(): Promise<void> {
    const name = formName.trim();
    const model = formModel.trim();
    if (!name || !model) {
      setFormError("Name and Model ID are required");
      return;
    }
    setFormError("");

    try {
      if (editingModel) {
        await fetchJSON(`/v1/models/${encodeURIComponent(editingModel.id)}`, {
          method: "PUT",
          body: JSON.stringify({
            name,
            provider: formProvider,
            model,
            baseUrl: formBaseUrl.trim(),
          }),
        });
      } else {
        await fetchJSON("/v1/models", {
          method: "POST",
          body: JSON.stringify({
            name,
            provider: formProvider,
            model,
            baseUrl: formBaseUrl.trim(),
          }),
        });
      }
      closeModal();
      await loadModels();
    } catch (err) {
      setFormError(String(err));
    }
  }

  async function handleDelete(id: string): Promise<void> {
    try {
      await fetchJSON(`/v1/models/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      setConfirmDelete(null);
      await loadModels();
    } catch (err) {
      console.error("[Models] delete error:", err);
    }
  }

  const filtered = models.filter((m) => {
    if (!search) return true;
    const query = search.toLowerCase();
    return (
      m.name.toLowerCase().includes(query) ||
      m.model.toLowerCase().includes(query) ||
      m.provider.toLowerCase().includes(query)
    );
  });

  if (loading) {
    return (
      <div className="settings-container">
        <h1 className="settings-header">Models</h1>
        <div className="models-loading">
          <div className="loading-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="settings-container">
      <div className="models-header">
        <div>
          <h1 className="settings-header" style={{ marginBottom: 4 }}>
            Models
          </h1>
          <p className="models-subtitle">
            Manage your model library. These models appear in the chat model
            picker.
          </p>
        </div>
        <button className="btn btn-primary btn-sm" onClick={openAddModal}>
          <Plus size={14} />
          Add Model
        </button>
      </div>

      {models.length > 0 && (
        <div className="models-search">
          <Search size={14} />
          <input
            className="models-search-input"
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search models..."
          />
        </div>
      )}

      {filtered.length === 0 ? (
        <div className="models-empty">
          {models.length === 0 ? (
            <>
              <p className="models-empty-text">No models yet</p>
              <p className="models-empty-hint">
                Add models here to use them in the chat model picker. Models are
                also auto-added when you configure one in Settings.
              </p>
            </>
          ) : (
            <p className="models-empty-text">No models match your search</p>
          )}
        </div>
      ) : (
        <div className="models-grid">
          {filtered.map((m) => (
            <div
              key={m.id}
              className="models-card"
              onClick={() => openEditModal(m)}
            >
              <div className="models-card-header">
                <div className="models-card-name">{m.name}</div>
                <span className="models-card-provider">
                  {providerLabel(m.provider)}
                </span>
              </div>
              <div className="models-card-model">{m.model}</div>
              {m.baseUrl && <div className="models-card-url">{m.baseUrl}</div>}
              <div className="models-card-footer">
                {confirmDelete === m.id ? (
                  <div
                    className="models-card-confirm"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <span>Delete?</span>
                    <button
                      className="btn btn-sm"
                      style={{ color: "var(--error)" }}
                      onClick={() => handleDelete(m.id)}
                    >
                      Yes
                    </button>
                    <button
                      className="btn btn-sm"
                      onClick={() => setConfirmDelete(null)}
                    >
                      No
                    </button>
                  </div>
                ) : (
                  <button
                    className="btn-ghost models-card-delete"
                    onClick={(e) => {
                      e.stopPropagation();
                      setConfirmDelete(m.id);
                    }}
                    title="Delete model"
                  >
                    <Trash size={14} />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {showModal && (
        <div className="models-modal-overlay" onClick={closeModal}>
          <div className="models-modal" onClick={(e) => e.stopPropagation()}>
            <div className="models-modal-header">
              <h2 className="models-modal-title">
                {editingModel ? "Edit Model" : "Add Model"}
              </h2>
              <button className="btn-ghost" onClick={closeModal}>
                <X size={18} />
              </button>
            </div>

            <div className="models-modal-body">
              <div className="models-modal-field">
                <label className="models-modal-label">Display Name</label>
                <input
                  className="input"
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="e.g. Claude Sonnet 4"
                  autoFocus
                />
              </div>

              <div className="models-modal-field">
                <label className="models-modal-label">Provider</label>
                <select
                  className="input"
                  value={formProvider}
                  onChange={(e) => setFormProvider(e.target.value)}
                >
                  {PROVIDERS.options.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="models-modal-field">
                <label className="models-modal-label">Model ID</label>
                <input
                  className="input"
                  type="text"
                  value={formModel}
                  onChange={(e) => setFormModel(e.target.value)}
                  placeholder="e.g. anthropic/claude-sonnet-4-20250514"
                />
              </div>

              <div className="models-modal-field">
                <label className="models-modal-label">
                  Base URL (optional)
                </label>
                <input
                  className="input"
                  type="text"
                  value={formBaseUrl}
                  onChange={(e) => setFormBaseUrl(e.target.value)}
                  placeholder="http://localhost:1234/v1"
                />
                <span className="models-modal-hint">
                  Only needed for custom/local providers
                </span>
              </div>

              {formError && <div className="models-error">{formError}</div>}
            </div>

            <div className="models-modal-footer">
              <button className="btn btn-secondary btn-sm" onClick={closeModal}>
                Cancel
              </button>
              <button className="btn btn-primary btn-sm" onClick={handleSave}>
                {editingModel ? "Update" : "Add Model"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default Models;
