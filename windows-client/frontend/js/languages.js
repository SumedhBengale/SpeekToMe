// Speaking-language selection and the language combobox.

function normalizeSpeakingLanguage() {
  if (!selectedLanguages.length) selectedLanguages = ['en'];
  if (!selectedLanguages.includes(speakingLanguage)) {
    speakingLanguage = selectedLanguages[0] || 'en';
  }
}

async function chooseSpeakingLanguage(choice) {
  if (languageUpdatePending || choice === speakingLanguage || !selectedLanguages.includes(choice)) return;
  languageUpdatePending = true;
  renderClientLanguageUI();
  try {
    speakingLanguage = await invoke('set_active_language', { language: choice });
    if (currentConfig) currentConfig.active_language = speakingLanguage;
    showToast(getLanguageName(speakingLanguage) + ' is now your current language.', 'Applies to the next recording', 'info');
  } catch (err) {
    showToast(String(err), 'Languages', 'error');
  } finally {
    languageUpdatePending = false;
    renderClientLanguageUI();
  }
}


function getLanguageName(code) {
  const item = SUPPORTED_LANGUAGES.find(l => l.code === code);
  return item ? item.name : code;
}

function renderClientLanguageUI() {
  normalizeSpeakingLanguage();
  const containers = [
    { chips: document.getElementById('client-lang-chips'), count: document.getElementById('client-lang-count'), prefix: 'client' },
    { chips: document.getElementById('onboard-lang-chips'), count: document.getElementById('onboard-lang-count'), prefix: 'onboard' }
  ];

  containers.forEach(({ chips: chipsContainer, count: countEl, prefix }) => {
    if (countEl) countEl.textContent = `${selectedLanguages.length} / 5`;
    const status = document.getElementById(`${prefix}-language-status`);
    if (status) status.textContent = `${getLanguageName(speakingLanguage)} is your current language. Changes apply to your next recording.`;
    const atLimit = selectedLanguages.length >= 5;
    const input = document.getElementById(`${prefix}-lang-search-input`);
    const add = document.getElementById(`${prefix}-add-lang-btn`);
    if (input) {
      input.disabled = languageUpdatePending || atLimit;
      input.title = atLimit ? 'Remove a language before adding another.' : '';
    }
    if (add) add.disabled = languageUpdatePending || atLimit;
    if (!chipsContainer) return;

    chipsContainer.replaceChildren();
    selectedLanguages.forEach(code => {
      const name = getLanguageName(code);
      const current = code === speakingLanguage;
      const chip = document.createElement('span');
      chip.className = 'lang-chip' + (current ? ' is-current' : '');
      chip.innerHTML = `<button type="button" class="lang-chip-star" aria-pressed="${current}" aria-label="${escapeHtml(current ? name + ' is your current language' : 'Use ' + name + ' for transcription')}" title="${escapeHtml(current ? 'Current language' : 'Make ' + name + ' current')}">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m12 3 2.78 5.63 6.22.91-4.5 4.39 1.06 6.2L12 17.2l-5.56 2.93 1.06-6.2L3 9.54l6.22-.91L12 3Z"></path></svg>
      </button><span>${escapeHtml(name)}</span>${current ? '<span class="lang-chip-current">Current</span>' : ''}
      <button type="button" class="lang-chip-remove" aria-label="Remove ${escapeHtml(name)}" title="${selectedLanguages.length <= 1 ? 'Keep at least one language' : 'Remove ' + escapeHtml(name)}">&times;</button>`;
      const star = chip.querySelector('.lang-chip-star');
      star.disabled = languageUpdatePending;
      star.addEventListener('click', () => chooseSpeakingLanguage(code));
      const remove = chip.querySelector('.lang-chip-remove');
      remove.disabled = languageUpdatePending || selectedLanguages.length <= 1;
      remove.addEventListener('click', () => {
        if (languageUpdatePending || selectedLanguages.length <= 1) return;
        selectedLanguages = selectedLanguages.filter(c => c !== code);
        saveLanguagesToBackend();
      });
      chipsContainer.appendChild(chip);
    });
  });
}

function setupLanguageCombobox(inputId, dropdownId, addBtnId) {
  const inputEl = document.getElementById(inputId);
  const dropdownEl = document.getElementById(dropdownId);
  const addBtn = document.getElementById(addBtnId);

  if (!inputEl || !dropdownEl) return;

  let highlightedIndex = -1;
  let currentMatches = [];

  function filterLanguages(query) {
    const q = (query || '').trim().toLowerCase();
    const available = SUPPORTED_LANGUAGES.filter(lang => !selectedLanguages.includes(lang.code));
    if (!q) {
      return available.slice(0, 15);
    }
    return available.filter(lang => 
      lang.name.toLowerCase().includes(q) || lang.code.toLowerCase().includes(q)
    ).slice(0, 20);
  }

  function renderDropdown(matches) {
    currentMatches = matches;
    highlightedIndex = -1;
    dropdownEl.innerHTML = '';

    if (matches.length === 0) {
      dropdownEl.innerHTML = '<div class="lang-dropdown-empty">No matching languages found</div>';
      dropdownEl.classList.remove('hidden');
      return;
    }

    matches.forEach((lang, idx) => {
      const item = document.createElement('div');
      item.className = 'lang-dropdown-item';
      item.dataset.index = idx;
      item.innerHTML = `<span>${escapeHtml(lang.name)}</span><span class="lang-code">${escapeHtml(lang.code)}</span>`;
      
      item.addEventListener('mousedown', (e) => {
        e.preventDefault(); // Prevent blur
        addSelectedLanguage(lang.code);
      });
      dropdownEl.appendChild(item);
    });

    dropdownEl.classList.remove('hidden');
  }

  function addSelectedLanguage(code) {
    if (!code || languageUpdatePending) return;
    if (selectedLanguages.length >= 5) {
      showToast("You can select at most 5 spoken languages.", "Languages", "warning");
      dropdownEl.classList.add('hidden');
      inputEl.value = '';
      return;
    }
    if (!selectedLanguages.includes(code)) {
      selectedLanguages.push(code);
      saveLanguagesToBackend();
    }
    inputEl.value = '';
    dropdownEl.classList.add('hidden');
  }

  inputEl.addEventListener('focus', () => {
    renderDropdown(filterLanguages(inputEl.value));
  });

  inputEl.addEventListener('input', () => {
    renderDropdown(filterLanguages(inputEl.value));
  });

  inputEl.addEventListener('keydown', (e) => {
    if (dropdownEl.classList.contains('hidden')) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        renderDropdown(filterLanguages(inputEl.value));
      }
      return;
    }

    const items = dropdownEl.querySelectorAll('.lang-dropdown-item');
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      highlightedIndex = Math.min(highlightedIndex + 1, items.length - 1);
      updateHighlight(items);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      highlightedIndex = Math.max(highlightedIndex - 1, 0);
      updateHighlight(items);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (highlightedIndex >= 0 && currentMatches[highlightedIndex]) {
        addSelectedLanguage(currentMatches[highlightedIndex].code);
      } else if (currentMatches.length > 0) {
        addSelectedLanguage(currentMatches[0].code);
      }
    } else if (e.key === 'Escape') {
      dropdownEl.classList.add('hidden');
    }
  });

  function updateHighlight(items) {
    items.forEach((item, idx) => {
      if (idx === highlightedIndex) {
        item.classList.add('active');
        item.scrollIntoView({ block: 'nearest' });
      } else {
        item.classList.remove('active');
      }
    });
  }

  inputEl.addEventListener('blur', () => {
    setTimeout(() => {
      dropdownEl.classList.add('hidden');
    }, 200);
  });

  if (addBtn) {
    addBtn.addEventListener('click', () => {
      const q = inputEl.value.trim().toLowerCase();
      const match = SUPPORTED_LANGUAGES.find(l => 
        !selectedLanguages.includes(l.code) && 
        (l.name.toLowerCase() === q || l.code.toLowerCase() === q)
      );
      if (match) {
        addSelectedLanguage(match.code);
      } else if (currentMatches.length > 0) {
        addSelectedLanguage(currentMatches[0].code);
      } else {
        showToast("Please search and select a language from the list.", "Languages", "warning");
      }
    });
  }
}

async function saveLanguagesToBackend() {
  if (languageUpdatePending) return;
  languageUpdatePending = true;
  renderClientLanguageUI();
  try {
    const updated = await invoke('update_languages', { languages: selectedLanguages });
    selectedLanguages = updated || selectedLanguages;
    normalizeSpeakingLanguage();
    if (currentConfig) {
      currentConfig.languages = [...selectedLanguages];
      currentConfig.active_language = speakingLanguage;
    }
    showToast('Languages saved. ' + getLanguageName(speakingLanguage) + ' is your current language.', 'Languages', 'info');
  } catch (err) {
    showToast(String(err || 'Failed to update language preferences.'), 'Languages', 'error');
    const saved = await invoke('get_config').catch(() => currentConfig || {});
    selectedLanguages = saved.languages?.length ? [...saved.languages] : ['en'];
    speakingLanguage = saved.active_language || selectedLanguages[0] || 'en';
  } finally {
    languageUpdatePending = false;
    renderClientLanguageUI();
  }
}
