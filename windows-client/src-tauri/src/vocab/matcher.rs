   use std::collections::{HashMap, HashSet};

pub const SINGLE_WORD_THRESHOLD: f64 = 0.88;
pub const TWO_WORD_THRESHOLD: f64 = 0.84;
pub const THREE_WORD_THRESHOLD: f64 = 0.80;
pub const MULTI_WORD_THRESHOLD: f64 = 0.78;

#[derive(Debug, Clone, PartialEq)]
pub struct CompiledTerm {
    pub canonical: String,
    pub normalized: String,
    pub token_count: usize,
    pub min_token_count: usize,
    pub max_token_count: usize,
    pub sub_tokens: Vec<String>,
    pub phonetic_key: String,
}

#[derive(Debug, Clone, Default)]
pub struct CompiledVocabulary {
    pub normalized_lookup: HashMap<String, String>,
    pub terms_by_count: HashMap<usize, Vec<CompiledTerm>>,
    pub terms_by_length: HashMap<usize, Vec<CompiledTerm>>,
    pub terms_by_phonetic: HashMap<String, Vec<CompiledTerm>>,
    pub terms: Vec<CompiledTerm>,
    pub max_token_count: usize,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Token {
    pub original: String,
    pub prefix: String,
    pub clean: String,
    pub suffix: String,
    pub normalized: String,
}

/// Normalizes a string by converting to lowercase and keeping only alphanumeric, '+', and '#' characters.
pub fn normalize_string(s: &str) -> String {
    s.chars()
        .filter_map(|c| {
            let lower = c.to_ascii_lowercase();
            if lower.is_ascii_alphanumeric() || lower == '+' || lower == '#' {
                Some(lower)
            } else {
                None
            }
        })
        .collect()
}

/// Tokenizes text into words in a single O(N) pass, preserving prefixes and suffixes for punctuation.
pub fn tokenize(text: &str) -> Vec<Token> {
    let mut tokens = Vec::new();
    let chars: Vec<char> = text.chars().collect();
    let n = chars.len();

    let is_word_char = |c: char| c.is_alphanumeric() || c == '+' || c == '#';
    let is_space = |c: char| c.is_whitespace();

    let mut i = 0;
    while i < n {
        while i < n && is_space(chars[i]) {
            i += 1;
        }
        if i >= n {
            break;
        }

        let start = i;
        while i < n && !is_space(chars[i]) {
            i += 1;
        }
        let end = i;

        let mut first_word_idx = None;
        let mut last_word_idx = None;

        for j in start..end {
            if is_word_char(chars[j]) {
                if first_word_idx.is_none() {
                    first_word_idx = Some(j);
                }
                last_word_idx = Some(j);
            }
        }

        match (first_word_idx, last_word_idx) {
            (Some(f), Some(l)) => {
                let prefix: String = chars[start..f].iter().collect();
                let clean: String = chars[f..=l].iter().collect();
                let suffix: String = chars[l + 1..end].iter().collect();
                let original: String = chars[start..end].iter().collect();
                let normalized = normalize_string(&clean);

                tokens.push(Token {
                    original,
                    prefix,
                    clean,
                    suffix,
                    normalized,
                });
            }
            _ => {
                let original: String = chars[start..end].iter().collect();
                tokens.push(Token {
                    original: original.clone(),
                    prefix: String::new(),
                    clean: String::new(),
                    suffix: original,
                    normalized: String::new(),
                });
            }
        }
    }

    tokens
}

/// Splits terms on symbols (. - _ / @ : ;) and CamelCase / PascalCase transitions.
pub fn extract_sub_tokens(s: &str) -> Vec<String> {
    let mut sub_tokens = Vec::new();
    let mut current = String::new();
    let runes: Vec<char> = s.chars().collect();
    let n = runes.len();

    let is_symbol = |c: char| {
        c == '.' || c == '-' || c == '_' || c == '/' || c == '@' || c == ':' || c == ';'
    };

    for i in 0..n {
        let r = runes[i];
        if is_symbol(r) || r.is_whitespace() {
            if !current.is_empty() {
                sub_tokens.push(current.clone());
                current.clear();
            }
            continue;
        }

        if i > 0 && !current.is_empty() {
            let prev = runes[i - 1];
            // Lowercase -> Uppercase (e.g. 'k' -> 'T' in SpeekTo)
            if prev.is_ascii_lowercase() && r.is_ascii_uppercase() {
                sub_tokens.push(current.clone());
                current.clear();
            } else if prev.is_ascii_uppercase()
                && r.is_ascii_uppercase()
                && i + 1 < n
                && runes[i + 1].is_ascii_lowercase()
            {
                // Acronym -> Word (e.g. 'CMS' -> 'Enterprise')
                sub_tokens.push(current.clone());
                current.clear();
            }
        }

        current.push(r);
    }

    if !current.is_empty() {
        sub_tokens.push(current);
    }

    sub_tokens
        .into_iter()
        .filter_map(|st| {
            let norm = normalize_string(&st);
            if norm.is_empty() {
                None
            } else {
                Some(norm)
            }
        })
        .collect()
}

/// Computes a simplified Double Metaphone phonetic key.
pub fn double_metaphone(s: &str) -> String {
    let norm = normalize_string(s);
    if norm.is_empty() {
        return String::new();
    }

    let runes: Vec<char> = norm.chars().collect();
    let n = runes.len();
    let mut sb = String::new();

    let is_vowel = |c: char| matches!(c, 'a' | 'e' | 'i' | 'o' | 'u' | 'y');

    let mut i = 0;
    while i < n {
        let r = runes[i];
        if i == 0 && is_vowel(r) {
            sb.push('A');
            i += 1;
            continue;
        }

        match r {
            'b' => {
                sb.push('P');
                if i + 1 < n && runes[i + 1] == 'b' {
                    i += 1;
                }
            }
            'c' => {
                if i + 1 < n && matches!(runes[i + 1], 'e' | 'i' | 'y') {
                    sb.push('S');
                } else if i + 1 < n && runes[i + 1] == 'h' {
                    sb.push('X');
                    i += 1;
                } else {
                    sb.push('K');
                }
                if i + 1 < n && matches!(runes[i + 1], 'c' | 'k') {
                    i += 1;
                }
            }
            'd' => {
                if i + 1 < n && runes[i + 1] == 'g' {
                    sb.push('J');
                    i += 1;
                } else {
                    sb.push('T');
                }
                if i + 1 < n && runes[i + 1] == 'd' {
                    i += 1;
                }
            }
            'f' => {
                sb.push('F');
                if i + 1 < n && runes[i + 1] == 'f' {
                    i += 1;
                }
            }
            'g' => {
                if i + 1 < n && matches!(runes[i + 1], 'e' | 'i' | 'y') {
                    sb.push('J');
                } else {
                    sb.push('K');
                }
                if i + 1 < n && matches!(runes[i + 1], 'g' | 'h') {
                    i += 1;
                }
            }
            'h' => {
                if i > 0 && !is_vowel(runes[i - 1]) {
                    // silent h after consonant
                } else if i + 1 < n && is_vowel(runes[i + 1]) {
                    sb.push('H');
                }
            }
            'j' => {
                sb.push('J');
            }
            'k' => {
                sb.push('K');
                if i + 1 < n && runes[i + 1] == 'k' {
                    i += 1;
                }
            }
            'l' => {
                sb.push('L');
                if i + 1 < n && runes[i + 1] == 'l' {
                    i += 1;
                }
            }
            'm' => {
                sb.push('M');
                if i + 1 < n && runes[i + 1] == 'm' {
                    i += 1;
                }
            }
            'n' => {
                sb.push('N');
                if i + 1 < n && runes[i + 1] == 'n' {
                    i += 1;
                }
            }
            'p' => {
                if i + 1 < n && runes[i + 1] == 'h' {
                    sb.push('F');
                    i += 1;
                } else {
                    sb.push('P');
                }
                if i + 1 < n && runes[i + 1] == 'p' {
                    i += 1;
                }
            }
            'q' => {
                sb.push('K');
            }
            'r' => {
                sb.push('R');
                if i + 1 < n && runes[i + 1] == 'r' {
                    i += 1;
                }
            }
            's' => {
                if i + 1 < n && runes[i + 1] == 'h' {
                    sb.push('X');
                    i += 1;
                } else {
                    sb.push('S');
                }
                if i + 1 < n && runes[i + 1] == 's' {
                    i += 1;
                }
            }
            't' => {
                if i + 1 < n && runes[i + 1] == 'h' {
                    sb.push('0');
                    i += 1;
                } else if i + 2 < n && runes[i + 1] == 'c' && runes[i + 2] == 'h' {
                    sb.push('X');
                    i += 2;
                } else {
                    sb.push('T');
                }
                if i + 1 < n && runes[i + 1] == 't' {
                    i += 1;
                }
            }
            'v' => {
                sb.push('F');
            }
            'w' => {
                if i + 1 < n && is_vowel(runes[i + 1]) {
                    sb.push('W');
                }
            }
            'x' => {
                sb.push_str("KS");
            }
            'y' => {
                if i + 1 < n && is_vowel(runes[i + 1]) {
                    sb.push('Y');
                }
            }
            'z' => {
                sb.push('S');
            }
            _ => {}
        }
        i += 1;
    }

    sb
}

/// Computes the Jaro-Winkler distance metric between two strings (0.0 to 1.0).
pub fn jaro_winkler(s1: &str, s2: &str) -> f64 {
    let r1: Vec<char> = s1.chars().collect();
    let r2: Vec<char> = s2.chars().collect();
    let l1 = r1.len();
    let l2 = r2.len();

    if l1 == 0 && l2 == 0 {
        return 1.0;
    }
    if l1 == 0 || l2 == 0 {
        return 0.0;
    }

    let match_window = (l1.max(l2) / 2).saturating_sub(1);
    let mut m1 = vec![false; l1];
    let mut m2 = vec![false; l2];
    let mut matches = 0;

    for i in 0..l1 {
        let start = i.saturating_sub(match_window);
        let end = (i + match_window + 1).min(l2);

        for j in start..end {
            if !m2[j] && r1[i] == r2[j] {
                m1[i] = true;
                m2[j] = true;
                matches += 1;
                break;
            }
        }
    }

    if matches == 0 {
        return 0.0;
    }

    let mut t = 0;
    let mut k = 0;
    for i in 0..l1 {
        if m1[i] {
            while !m2[k] {
                k += 1;
            }
            if r1[i] != r2[k] {
                t += 1;
            }
            k += 1;
        }
    }

    let transpositions = t / 2;
    let m = matches as f64;
    let jaro = (1.0 / 3.0)
        * (m / l1 as f64 + m / l2 as f64 + (m - transpositions as f64) / m);

    let max_prefix = 4.min(l1.min(l2));
    let mut prefix_len = 0;
    for i in 0..max_prefix {
        if r1[i] == r2[i] {
            prefix_len += 1;
        } else {
            break;
        }
    }

    let p = 0.1;
    jaro + prefix_len as f64 * p * (1.0 - jaro)
}

fn is_safe_auto_expand_sub_word(word: &str) -> bool {
    let norm = normalize_string(word);
    if norm.len() < 3 {
        return false;
    }

    let connectors = [
        "the", "and", "for", "with", "from", "that", "this", "have", "are", "was", "were", "been",
        "you", "your", "their", "there", "they", "what", "when", "where", "who", "how", "why",
        "which", "will", "would", "could", "should", "not", "but", "can", "all",
    ];

    !connectors.contains(&norm.as_str())
}

/// Compiles a list of canonical vocabulary terms into an indexed, fast-lookup structure.
pub fn compile_vocabulary(canonical_terms: &[String]) -> CompiledVocabulary {
    let mut normalized_lookup = HashMap::new();
    let mut compiled_terms = Vec::with_capacity(canonical_terms.len());
    let mut terms_by_count: HashMap<usize, Vec<CompiledTerm>> = HashMap::new();
    let mut terms_by_length: HashMap<usize, Vec<CompiledTerm>> = HashMap::new();
    let mut terms_by_phonetic: HashMap<String, Vec<CompiledTerm>> = HashMap::new();

    for canonical in canonical_terms {
        let trimmed = canonical.trim();
        if trimmed.is_empty() {
            continue;
        }

        let tokens = tokenize(trimmed);
        let normalized_tokens: Vec<String> = tokens.iter().map(|t| t.normalized.clone()).collect();
        let computed_normalized = normalized_tokens.join("");

        let sub_tokens = extract_sub_tokens(trimmed);
        let joined_sub_tokens = sub_tokens.join("");

        normalized_lookup.insert(computed_normalized.clone(), trimmed.to_string());
        if !joined_sub_tokens.is_empty() && joined_sub_tokens != computed_normalized {
            normalized_lookup.insert(joined_sub_tokens.clone(), trimmed.to_string());
        }

        let mut min_count = normalized_tokens.len();
        let mut max_count = normalized_tokens.len();
        if sub_tokens.len() > max_count {
            max_count = sub_tokens.len();
        }
        if !sub_tokens.is_empty() && sub_tokens.len() < min_count {
            min_count = sub_tokens.len();
        }

        let phonetic_key = double_metaphone(&computed_normalized);

        let ct = CompiledTerm {
            canonical: trimmed.to_string(),
            normalized: computed_normalized.clone(),
            token_count: normalized_tokens.len(),
            min_token_count: min_count,
            max_token_count: max_count,
            sub_tokens: sub_tokens.clone(),
            phonetic_key: phonetic_key.clone(),
        };

        compiled_terms.push(ct.clone());

        let mut counts_to_register = HashSet::new();
        counts_to_register.insert(normalized_tokens.len());
        if !sub_tokens.is_empty() {
            counts_to_register.insert(sub_tokens.len());
        }

        for c in counts_to_register {
            terms_by_count.entry(c).or_default().push(ct.clone());
        }

        let term_len = computed_normalized.len();
        terms_by_length.entry(term_len).or_default().push(ct.clone());
        if !joined_sub_tokens.is_empty() && joined_sub_tokens.len() != term_len {
            terms_by_length
                .entry(joined_sub_tokens.len())
                .or_default()
                .push(ct.clone());
        }

        if !phonetic_key.is_empty() {
            terms_by_phonetic
                .entry(phonetic_key)
                .or_default()
                .push(ct.clone());
        }

        // Sub-word auto-expansion for multi-word entries
        if normalized_tokens.len() > 1 {
            for tok in &tokens {
                if !tok.clean.is_empty() && is_safe_auto_expand_sub_word(&tok.clean) {
                    let sub_norm = normalize_string(&tok.clean);
                    if !sub_norm.is_empty() && !normalized_lookup.contains_key(&sub_norm) {
                        let mut chars = tok.clean.chars();
                        let sub_canonical = match chars.next() {
                            Some(c) => c.to_uppercase().collect::<String>() + chars.as_str(),
                            None => tok.clean.clone(),
                        };

                        normalized_lookup.insert(sub_norm.clone(), sub_canonical.clone());
                        let sub_phonetic = double_metaphone(&sub_norm);
                        let sub_ct = CompiledTerm {
                            canonical: sub_canonical,
                            normalized: sub_norm.clone(),
                            token_count: 1,
                            min_token_count: 1,
                            max_token_count: 1,
                            sub_tokens: vec![sub_norm.clone()],
                            phonetic_key: sub_phonetic.clone(),
                        };

                        compiled_terms.push(sub_ct.clone());
                        terms_by_count.entry(1).or_default().push(sub_ct.clone());
                        terms_by_length
                            .entry(sub_norm.len())
                            .or_default()
                            .push(sub_ct.clone());
                        if !sub_phonetic.is_empty() {
                            terms_by_phonetic
                                .entry(sub_phonetic)
                                .or_default()
                                .push(sub_ct);
                        }
                    }
                }
            }
        }
    }

    let max_token_count = compiled_terms
        .iter()
        .map(|t| t.max_token_count)
        .max()
        .unwrap_or(0);

    CompiledVocabulary {
        normalized_lookup,
        terms_by_count,
        terms_by_length,
        terms_by_phonetic,
        terms: compiled_terms,
        max_token_count,
    }
}

/// Normalizes transcript text using exact, normalized, and multi-stage fuzzy vocabulary matching.
pub fn normalize_with_vocabulary(text: &str, vocab: &CompiledVocabulary) -> String {
    if vocab.terms.is_empty() || text.trim().is_empty() {
        return text.to_string();
    }

    let tokens = tokenize(text);
    if tokens.is_empty() {
        return text.to_string();
    }

    let n = tokens.len();
    let max_k = vocab.max_token_count.max(1);
    let mut result_tokens = Vec::new();

    let mut i = 0;
    while i < n {
        let mut matched = false;

        // Stage 1: Exact and Normalized N-Gram Lookup (Greedy lookahead from max_k down to 1)
        for k in (1..=max_k).rev() {
            if i + k <= n {
                let joined_norm: String = (0..k).map(|j| tokens[i + j].normalized.as_str()).collect();
                if let Some(canonical) = vocab.normalized_lookup.get(&joined_norm) {
                    let replaced_token = Token {
                        original: format!("{}{}{}", tokens[i].prefix, canonical, tokens[i + k - 1].suffix),
                        prefix: tokens[i].prefix.clone(),
                        clean: canonical.clone(),
                        suffix: tokens[i + k - 1].suffix.clone(),
                        normalized: joined_norm,
                    };
                    result_tokens.push(replaced_token);
                    i += k;
                    matched = true;
                    break;
                }
            }
        }

        if matched {
            continue;
        }

        // Stage 2, 3, 4: Fuzzy Matching
        let mut best_sim = 0.0;
        let mut best_canonical: Option<String> = None;
        let mut best_k = 0;

        for k in (1..=max_k).rev() {
            if i + k <= n {
                let joined_norm: String = (0..k).map(|j| tokens[i + j].normalized.as_str()).collect();
                if joined_norm.is_empty() {
                    continue;
                }

                let threshold = match k {
                    1 => SINGLE_WORD_THRESHOLD,
                    2 => TWO_WORD_THRESHOLD,
                    3 => THREE_WORD_THRESHOLD,
                    _ => MULTI_WORD_THRESHOLD,
                };

                // Stage 2: Token-count candidate pool
                if let Some(candidates) = vocab.terms_by_count.get(&k) {
                    for term in candidates {
                        let len_diff = (joined_norm.len() as isize - term.normalized.len() as isize).unsigned_abs();
                        let match_prefix = if joined_norm.len() >= 2 && term.normalized.len() >= 2 {
                            joined_norm[..2] == term.normalized[..2]
                        } else if !joined_norm.is_empty() && !term.normalized.is_empty() {
                            joined_norm[..1] == term.normalized[..1]
                        } else {
                            false
                        };

                        let mut sub_token_match = true;
                        if k >= 2 && term.sub_tokens.len() == k {
                            for j in 0..k {
                                let tok_sim = jaro_winkler(&tokens[i + j].normalized, &term.sub_tokens[j]);
                                let min_tok_sim = if term.sub_tokens[j].len() <= 2 { 0.85 } else { 0.65 };
                                if tok_sim < min_tok_sim {
                                    sub_token_match = false;
                                    break;
                                }
                            }
                        }

                        if len_diff <= 2 * k && match_prefix && sub_token_match {
                            let sim = jaro_winkler(&joined_norm, &term.normalized);
                            if sim >= threshold && sim > best_sim {
                                best_sim = sim;
                                best_canonical = Some(term.canonical.clone());
                                best_k = k;
                            }
                        }
                    }
                }

                // Stage 3: Length-delta candidate pool
                if best_canonical.is_none() {
                    let norm_len = joined_norm.len() as isize;
                    for delta in -2..=2 {
                        let target_len = norm_len + delta;
                        if target_len <= 0 {
                            continue;
                        }
                        if let Some(candidates) = vocab.terms_by_length.get(&(target_len as usize)) {
                            for term in candidates {
                                if k > term.max_token_count {
                                    continue;
                                }
                                let len_diff = (joined_norm.len() as isize - term.normalized.len() as isize).unsigned_abs();
                                let max_len = joined_norm.len().max(term.normalized.len());
                                let len_diff_ratio = len_diff as f64 / max_len as f64;

                                let match_prefix = if !joined_norm.is_empty() && !term.normalized.is_empty() {
                                    joined_norm[..1] == term.normalized[..1]
                                } else {
                                    false
                                };

                                let mut sub_token_match = true;
                                if k >= 2 && term.sub_tokens.len() == k {
                                    for j in 0..k {
                                        let tok_sim = jaro_winkler(&tokens[i + j].normalized, &term.sub_tokens[j]);
                                        let min_tok_sim = if term.sub_tokens[j].len() <= 2 { 0.85 } else { 0.65 };
                                        if tok_sim < min_tok_sim {
                                            sub_token_match = false;
                                            break;
                                        }
                                    }
                                }

                                if match_prefix && len_diff_ratio <= 0.15 && sub_token_match {
                                    let sim = jaro_winkler(&joined_norm, &term.normalized);
                                    if sim >= threshold && sim > best_sim {
                                        best_sim = sim;
                                        best_canonical = Some(term.canonical.clone());
                                        best_k = k;
                                    }
                                }
                            }
                        }
                    }
                }

                // Stage 4: Phonetic candidate pool
                if best_canonical.is_none() {
                    let window_phonetic = double_metaphone(&joined_norm);
                    if !window_phonetic.is_empty() {
                        if let Some(candidates) = vocab.terms_by_phonetic.get(&window_phonetic) {
                            for term in candidates {
                                if k > term.max_token_count {
                                    continue;
                                }
                                let len_diff = (joined_norm.len() as isize - term.normalized.len() as isize).unsigned_abs();
                                let max_len = joined_norm.len().max(term.normalized.len());
                                let len_diff_ratio = len_diff as f64 / max_len as f64;

                                let match_prefix = if !joined_norm.is_empty() && !term.normalized.is_empty() {
                                    joined_norm[..1] == term.normalized[..1]
                                } else {
                                    false
                                };

                                let mut sub_token_match = true;
                                if k >= 2 && term.sub_tokens.len() == k {
                                    for j in 0..k {
                                        let tok_sim = jaro_winkler(&tokens[i + j].normalized, &term.sub_tokens[j]);
                                        let min_tok_sim = if term.sub_tokens[j].len() <= 2 { 0.85 } else { 0.65 };
                                        if tok_sim < min_tok_sim {
                                            sub_token_match = false;
                                            break;
                                        }
                                    }
                                }

                                if match_prefix && len_diff_ratio <= 0.15 && sub_token_match {
                                    let sim = jaro_winkler(&joined_norm, &term.normalized);
                                    if (sim >= 0.75 || sim >= threshold - 0.05) && sim > best_sim {
                                        best_sim = sim;
                                        best_canonical = Some(term.canonical.clone());
                                        best_k = k;
                                    }
                                }
                            }
                        }
                    }
                }

                if best_canonical.is_some() {
                    break;
                }
            }
        }

        if let Some(canonical) = best_canonical {
            let replaced_token = Token {
                original: format!("{}{}{}", tokens[i].prefix, canonical, tokens[i + best_k - 1].suffix),
                prefix: tokens[i].prefix.clone(),
                clean: canonical.clone(),
                suffix: tokens[i + best_k - 1].suffix.clone(),
                normalized: normalize_string(&canonical),
            };
            result_tokens.push(replaced_token);
            i += best_k;
        } else {
            result_tokens.push(tokens[i].clone());
            i += 1;
        }
    }

    result_tokens
        .into_iter()
        .map(|t| t.original)
        .collect::<Vec<String>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_exact_and_fuzzy_normalization() {
        let terms = vec![
            "SpeekTo.Me".to_string(),
            "PostgreSQL".to_string(),
            "Google DeepMind".to_string(),
            "GraphQL".to_string(),
        ];
        let vocab = compile_vocabulary(&terms);

        // Exact match
        assert_eq!(
            normalize_with_vocabulary("Welcome to speek to me today", &vocab),
            "Welcome to SpeekTo.Me today"
        );

        // Sub-word & multi-word match
        assert_eq!(
            normalize_with_vocabulary("We use google deep mind algorithms", &vocab),
            "We use Google DeepMind algorithms"
        );

        // Fuzzy match with minor typo
        assert_eq!(
            normalize_with_vocabulary("Connecting to posgresql database...", &vocab),
            "Connecting to PostgreSQL database..."
        );
    }
}
