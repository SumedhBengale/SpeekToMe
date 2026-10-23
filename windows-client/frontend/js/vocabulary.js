// Custom vocabulary settings.

function showVocabLoading() {
  const loadingEl = document.getElementById('vocab-loading-state');
  const emptyEl = document.getElementById('vocab-empty-msg');
  const listEl = document.getElementById('vocab-list');
  const warningEl = document.getElementById('vocab-warning');

  if (loadingEl) {
    loadingEl.classList.remove('hidden');
    loadingEl.style.display = 'flex';
  }
  if (emptyEl) {
    emptyEl.classList.add('hidden');
    emptyEl.style.display = 'none';
  }
  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
  if (warningEl) {
    warningEl.classList.add('hidden');
  }
}

function hideVocabLoading() {
  const loadingEl = document.getElementById('vocab-loading-state');
  if (loadingEl) {
    loadingEl.classList.add('hidden');
    loadingEl.style.display = 'none';
  }
}

async function loadVocabulary() {
  const vocabList = document.getElementById('vocab-list');
  const vocabEmpty = document.getElementById('vocab-empty-msg');
  const vocabWarning = document.getElementById('vocab-warning');
  if (!vocabList) return;

  showVocabLoading();

  try {
    const terms = await invoke('get_vocabulary');
    const limit = isUserPro(currentUserInfo) ? 1000 : 20;
    if (vocabWarning) {
      const count = terms?.length || 0;
      vocabWarning.textContent = `${Math.min(count, limit)} of ${limit} terms active on your plan.` +
        (count > limit ? ` All ${count} saved terms are preserved; only the first ${limit} alphabetically are active.` : '');
      vocabWarning.classList.remove('hidden');
    }
    hideVocabLoading();
    vocabList.innerHTML = '';

    if (!terms || terms.length === 0) {
      if (vocabEmpty) {
        vocabEmpty.classList.remove('hidden');
        vocabEmpty.style.display = 'flex';
      }
      vocabList.classList.add('hidden');
      vocabList.style.display = 'none';
      return;
    }

    if (vocabEmpty) {
      vocabEmpty.classList.add('hidden');
      vocabEmpty.style.display = 'none';
    }
    vocabList.classList.remove('hidden');
    vocabList.style.display = 'flex';

    terms.forEach((term, index) => {
      const li = document.createElement('li');
      li.className = 'vocab-item';

      const textSpan = document.createElement('div');
      textSpan.className = 'vocab-item-text';

      const termName = document.createElement('span');
      termName.className = 'vocab-term-text';
      termName.textContent = term.canonical_term;

      const normSpan = document.createElement('span');
      normSpan.className = 'vocab-term-normalized';
      normSpan.textContent = index >= limit ? "Saved; inactive on this plan" : `(${term.normalized_term})`;

      textSpan.appendChild(termName);
      textSpan.appendChild(normSpan);

      const deleteBtn = document.createElement('button');
      deleteBtn.type = 'button';
      deleteBtn.className = 'vocab-delete-btn';
      deleteBtn.title = `Delete "${term.canonical_term}"`;
      deleteBtn.setAttribute('aria-label', `Delete ${term.canonical_term}`);
      deleteBtn.innerHTML = `
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="3 6 5 6 21 6"></polyline>
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          <line x1="10" y1="11" x2="10" y2="17"></line>
          <line x1="14" y1="11" x2="14" y2="17"></line>
        </svg>
      `;

      deleteBtn.addEventListener('click', async () => {
        const confirmed = await showConfirmDialog({
          title: "Delete Vocabulary Term?",
          message: `Are you sure you want to remove "${term.canonical_term}" from your custom vocabulary?`,
          confirmText: "Delete",
          cancelText: "Cancel",
          isDanger: true
        });
        if (!confirmed) return;

        deleteBtn.disabled = true;
        try {
          await invoke('delete_vocabulary_term', { term: term.canonical_term });
          showToast(`Removed "${term.canonical_term}"`, "Vocabulary", "info");
          await loadVocabulary();
        } catch (err) {
          console.error("Failed to delete term:");
          showToast(err, "Failed to delete term", "error");
          deleteBtn.disabled = false;
        }
      });

      li.appendChild(textSpan);
      li.appendChild(deleteBtn);
      vocabList.appendChild(li);
    });
  } catch (err) {
    hideVocabLoading();
    console.error("Failed to load vocabulary:");
    if (vocabEmpty) {
      const emptyText = vocabEmpty.querySelector('.vocab-empty-text');
      if (emptyText) {
        emptyText.textContent = `Error loading vocabulary: ${err}`;
      } else {
        vocabEmpty.textContent = `Error loading vocabulary: ${err}`;
      }
      vocabEmpty.classList.remove('hidden');
      vocabEmpty.style.display = 'flex';
    }
  }
}

// Initialization is registered once, after all controllers are defined.

// ==========================================
// Meetings & Notes Feature Controller
// ==========================================
