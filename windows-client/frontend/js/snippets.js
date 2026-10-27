// Snippets: saved text the user inserts by saying its name while dictating.

let editingSnippetId = null;

function snippetElements() {
  return {
    form: document.getElementById('snippet-form'),
    name: document.getElementById('snippet-name'),
    content: document.getElementById('snippet-content'),
    save: document.getElementById('snippet-save-btn'),
    cancel: document.getElementById('snippet-cancel-btn'),
    error: document.getElementById('snippet-form-error'),
    count: document.getElementById('snippets-count'),
    loading: document.getElementById('snippets-loading'),
    empty: document.getElementById('snippets-empty'),
    list: document.getElementById('snippets-list'),
  };
}

function resetSnippetForm() {
  const el = snippetElements();
  editingSnippetId = null;
  el.form.reset();
  el.error.textContent = '';
  el.save.textContent = 'Save snippet';
  el.cancel.classList.add('hidden');
}

function editSnippet(snippet) {
  const el = snippetElements();
  editingSnippetId = snippet.id;
  el.name.value = snippet.name;
  el.content.value = snippet.content;
  el.error.textContent = '';
  el.save.textContent = 'Save changes';
  el.cancel.classList.remove('hidden');
  el.name.focus();
}

function renderSnippet(snippet) {
  const li = document.createElement('li');
  li.className = 'vocab-item snippet-item' + (snippet.active ? '' : ' is-inactive');

  const text = document.createElement('div');
  text.className = 'snippet-item-text';
  const name = document.createElement('span');
  name.className = 'vocab-term-text';
  name.textContent = snippet.name;
  text.appendChild(name);
  if (!snippet.active) {
    const badge = document.createElement('span');
    badge.className = 'snippet-inactive-badge';
    badge.textContent = 'Inactive on your plan';
    text.appendChild(badge);
  }
  const content = document.createElement('p');
  content.className = 'snippet-content-preview';
  content.textContent = snippet.content;
  text.appendChild(content);

  const actions = document.createElement('div');
  actions.className = 'snippet-item-actions';
  const edit = document.createElement('button');
  edit.type = 'button';
  edit.className = 'snippet-secondary-btn';
  edit.textContent = 'Edit';
  edit.setAttribute('aria-label', `Edit ${snippet.name}`);
  edit.addEventListener('click', () => editSnippet(snippet));

  const remove = document.createElement('button');
  remove.type = 'button';
  remove.className = 'vocab-delete-btn';
  remove.title = `Delete "${snippet.name}"`;
  remove.setAttribute('aria-label', `Delete ${snippet.name}`);
  remove.innerHTML = `
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <polyline points="3 6 5 6 21 6"></polyline>
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
    </svg>`;
  remove.addEventListener('click', async () => {
    const confirmed = await showConfirmDialog({
      title: 'Delete snippet?',
      message: `"${snippet.name}" will no longer be inserted when you say it.`,
      confirmText: 'Delete',
      cancelText: 'Cancel',
      isDanger: true
    });
    if (!confirmed) return;
    remove.disabled = true;
    try {
      await invoke('delete_snippet', { id: snippet.id });
      if (editingSnippetId === snippet.id) resetSnippetForm();
      showToast(`Deleted "${snippet.name}"`, 'Snippets', 'info');
      await loadSnippets();
    } catch (err) {
      showToast(err, 'Could not delete snippet', 'error');
      remove.disabled = false;
    }
  });

  actions.appendChild(edit);
  actions.appendChild(remove);
  li.appendChild(text);
  li.appendChild(actions);
  return li;
}

async function loadSnippets() {
  const el = snippetElements();
  if (!el.list) return;
  el.loading.classList.remove('hidden');
  el.empty.classList.add('hidden');
  el.list.innerHTML = '';
  try {
    const result = await invoke('get_snippets');
    const snippets = result?.snippets || [];
    const limit = result?.limit || 0;
    const active = snippets.filter(s => s.active).length;
    el.count.textContent = `${active} of ${limit} snippets active on your plan.` +
      (snippets.length > limit ? ` All ${snippets.length} are kept; the newest ones are inactive until you upgrade or delete some.` : '');
    snippets.forEach(snippet => el.list.appendChild(renderSnippet(snippet)));
    el.empty.classList.toggle('hidden', snippets.length > 0);
  } catch (err) {
    el.count.textContent = '';
    el.empty.querySelector('.vocab-empty-text').textContent = `Could not load snippets: ${err}`;
    el.empty.classList.remove('hidden');
  } finally {
    el.loading.classList.add('hidden');
  }
}

function initSnippets() {
  const el = snippetElements();
  if (!el.form) return;
  el.form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const name = el.name.value.trim();
    const content = el.content.value;
    if (!name || !content.trim()) {
      el.error.textContent = 'Add a name and the text it inserts.';
      return;
    }
    el.save.disabled = true;
    el.error.textContent = '';
    const wasEditing = editingSnippetId !== null;
    try {
      await invoke('save_snippet', { id: editingSnippetId, name, content });
      resetSnippetForm();
      showToast(wasEditing ? `Updated "${name}"` : `Say "${name}" to insert it`, 'Snippet saved', 'success');
      await loadSnippets();
    } catch (err) {
      el.error.textContent = String(err);
    } finally {
      el.save.disabled = false;
    }
  });
  el.cancel.addEventListener('click', resetSnippetForm);
  el.content.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) el.form.requestSubmit();
  });
}

document.addEventListener('DOMContentLoaded', initSnippets);
