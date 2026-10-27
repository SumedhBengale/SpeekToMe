// Snippets: saved text the user inserts by saying its name while dictating.
import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from '../utils/api';

const emptyForm = { id: null, name: '', content: '' };

async function readError(res, fallback) {
  try {
    const body = await res.json();
    return body.error || fallback;
  } catch {
    return fallback;
  }
}

export default function SnippetsPanel({ token }) {
  const [snippets, setSnippets] = useState([]);
  const [limit, setLimit] = useState(0);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [form, setForm] = useState(emptyForm);
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/v1/snippets`, { headers: { Authorization: `Bearer ${token}` } });
      if (!res.ok) throw new Error(await readError(res, 'Snippets are unavailable right now.'));
      const data = await res.json();
      setSnippets(data.snippets || []);
      setLimit(data.limit || 0);
      setLoadError('');
    } catch (err) {
      setLoadError(err.message);
    } finally {
      setLoaded(true);
    }
  }, [token]);

  useEffect(() => { load(); }, [load]);

  const save = async (e) => {
    e.preventDefault();
    if (!form.name.trim() || !form.content.trim()) {
      setFormError('Add a name and the text it inserts.');
      return;
    }
    setSaving(true);
    setFormError('');
    try {
      const res = await fetch(`${API_BASE}/v1/snippets${form.id ? `/${form.id}` : ''}`, {
        method: form.id ? 'PUT' : 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: form.name, content: form.content }),
      });
      if (!res.ok) throw new Error(await readError(res, 'The snippet could not be saved.'));
      setForm(emptyForm);
      await load();
    } catch (err) {
      setFormError(err.message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (snippet) => {
    if (!window.confirm(`Delete "${snippet.name}"? It will no longer be inserted when you say it.`)) return;
    setDeleting(snippet.id);
    try {
      const res = await fetch(`${API_BASE}/v1/snippets/${snippet.id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!res.ok && res.status !== 404) throw new Error(await readError(res, 'The snippet could not be deleted.'));
      if (form.id === snippet.id) setForm(emptyForm);
      await load();
    } catch (err) {
      setFormError(err.message);
    } finally {
      setDeleting(null);
    }
  };

  const active = snippets.filter((s) => s.active).length;

  return (
    <div className="card">
      <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M8 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2" /><path d="M14 3h7v7" /><path d="M21 3l-9 9" /><path d="M8 12h3" /><path d="M8 16h6" /></svg>
        Snippets
      </h3>
      <p className="snippets-intro">
        Save text you type often, then say its name while dictating to insert it. Say the name on its own, or
        &ldquo;insert my address&rdquo;, to add the snippet. In a sentence like &ldquo;I&rsquo;ll email you my
        address&rdquo;, the words stay as you said them.
      </p>

      <form onSubmit={save} className="snippet-form">
        <label htmlFor="snippet-name" className="snippet-label">Name you&rsquo;ll say</label>
        <input
          id="snippet-name"
          type="text"
          className="auth-input"
          maxLength={60}
          placeholder="e.g. my address"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          required
        />
        <label htmlFor="snippet-content" className="snippet-label">Text it inserts</label>
        <textarea
          id="snippet-content"
          className="auth-input snippet-textarea"
          maxLength={5000}
          rows={4}
          placeholder="e.g. 221B Baker Street, London NW1 6XE"
          value={form.content}
          onChange={(e) => setForm({ ...form, content: e.target.value })}
          required
        />
        {formError && <div className="auth-error">{formError}</div>}
        <div className="snippet-form-actions">
          {form.id && (
            <button type="button" className="btn" onClick={() => { setForm(emptyForm); setFormError(''); }}>
              Cancel
            </button>
          )}
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? <div className="spinner"></div> : form.id ? 'Save changes' : 'Save snippet'}
          </button>
        </div>
      </form>

      {loaded && !loadError && (
        <p className="setting-hint">
          {active} of {limit.toLocaleString()} snippets active on your plan.
          {snippets.length > limit && ` All ${snippets.length} are kept; the newest ones are inactive until you upgrade or delete some.`}
        </p>
      )}
      {loadError && <div className="auth-error">{loadError}</div>}

      {loaded && !loadError && snippets.length === 0 && (
        <p className="snippets-empty">No snippets yet. Save one above, then say its name on its own while dictating.</p>
      )}
      {snippets.length > 0 && (
        <ul className="snippet-list">
          {snippets.map((snippet) => (
            <li key={snippet.id} className={`snippet-row${snippet.active ? '' : ' is-inactive'}`}>
              <div className="snippet-row-text">
                <strong>{snippet.name}</strong>
                {!snippet.active && <span className="badge-pill">Inactive on your plan</span>}
                <p className="snippet-row-content">{snippet.content}</p>
              </div>
              <div className="snippet-row-actions">
                <button
                  type="button"
                  className="btn btn-sm"
                  onClick={() => { setForm({ id: snippet.id, name: snippet.name, content: snippet.content }); setFormError(''); }}
                >
                  Edit
                </button>
                <button
                  type="button"
                  className="btn btn-danger btn-sm"
                  disabled={deleting === snippet.id}
                  onClick={() => remove(snippet)}
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
