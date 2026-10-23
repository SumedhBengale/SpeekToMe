//! Keeps the session token in the operating system's credential store
//! (Windows Credential Manager, macOS Keychain, Secret Service on Linux)
//! instead of config.json. Where no credential store is available the token
//! stays in the owner-only config file, as before.
use std::sync::Mutex;

const SERVICE: &str = "SpeekTo.Me";
const ACCOUNT: &str = "session";

/// The last token written, so unchanged settings saves skip the store.
static STORED: Mutex<Option<String>> = Mutex::new(None);

#[cfg(not(test))]
fn entry() -> Option<keyring::Entry> {
    keyring::Entry::new(SERVICE, ACCOUNT).ok()
}

#[cfg(not(test))]
fn read() -> Option<String> {
    entry()?
        .get_password()
        .ok()
        .filter(|token| !token.is_empty())
}

#[cfg(not(test))]
fn write(token: &str) -> bool {
    entry().is_some_and(|entry| entry.set_password(token).is_ok())
}

#[cfg(not(test))]
fn remove() {
    if let Some(entry) = entry() {
        let _ = entry.delete_password();
    }
}

// Tests never touch the developer's real credential store.
#[cfg(test)]
static TEST_STORE: Mutex<Option<String>> = Mutex::new(None);
#[cfg(test)]
fn read() -> Option<String> {
    TEST_STORE.lock().unwrap_or_else(|e| e.into_inner()).clone()
}
#[cfg(test)]
fn write(token: &str) -> bool {
    *TEST_STORE.lock().unwrap_or_else(|e| e.into_inner()) = Some(token.to_string());
    true
}
#[cfg(test)]
fn remove() {
    TEST_STORE.lock().unwrap_or_else(|e| e.into_inner()).take();
}

pub fn load() -> Option<String> {
    let token = read();
    *STORED.lock().unwrap_or_else(|e| e.into_inner()) = token.clone();
    token
}

/// Saves the token and reports whether the credential store now holds it.
pub fn store(token: &str) -> bool {
    let mut stored = STORED.lock().unwrap_or_else(|e| e.into_inner());
    if stored.as_deref() == Some(token) {
        return true;
    }
    if write(token) {
        *stored = Some(token.to_string());
        return true;
    }
    false
}

pub fn clear() {
    let mut stored = STORED.lock().unwrap_or_else(|e| e.into_inner());
    if stored.take().is_some() || read().is_some() {
        remove();
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn store_load_and_clear_round_trip() {
        super::clear();
        assert!(super::store("token-a"));
        assert_eq!(super::load().as_deref(), Some("token-a"));
        super::clear();
        assert_eq!(super::load(), None);
    }
}
