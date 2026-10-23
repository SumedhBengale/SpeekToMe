//! Fail closed when identity or recording ownership is unknown. Never match time.
use crate::LocalMeetingRecord;
use sha2::{Digest, Sha256};
use std::path::{Path, PathBuf};
pub fn owns(record: &LocalMeetingRecord, user: i64, token: &str) -> bool {
    if user <= 0 {
        return false;
    }
    match record.owner_user_id {
        Some(owner) => owner == user,
        None => {
            record.token_fingerprint.as_deref()
                == Some(format!("{:x}", Sha256::digest(token.as_bytes())).as_str())
        }
    }
}
pub fn resolve(
    records: &[LocalMeetingRecord],
    client_id: &str,
    user: i64,
    token: &str,
    root: &Path,
) -> Result<PathBuf, String> {
    if client_id.is_empty() {
        return Err("Meeting recording identity unavailable".into());
    }
    let mut matching = records
        .iter()
        .filter(|r| r.id == client_id && owns(r, user, token));
    let record = matching
        .next()
        .ok_or("No recording for this meeting and account")?;
    if matching.next().is_some() {
        return Err("Duplicate recording identity; playback refused".into());
    }
    let root = root
        .canonicalize()
        .map_err(|_| "Recording folder unavailable")?;
    let file = Path::new(&record.filepath)
        .canonicalize()
        .map_err(|_| "Local recording unavailable")?;
    if !file.starts_with(&root)
        || !file.is_file()
        || file.extension().and_then(|s| s.to_str()) != Some("ogg")
    {
        return Err("Invalid local recording path".into());
    }
    Ok(file)
}
#[cfg(test)]
mod tests {
    use super::*;
    fn record(id: &str, user: Option<i64>) -> LocalMeetingRecord {
        LocalMeetingRecord {
            id: id.into(),
            owner_user_id: user,
            owner_email: Some("same@example.com".into()),
            token_fingerprint: None,
            language: None,
            title: "Meeting".into(),
            created_at: 100,
            filepath: String::new(),
            status: "recorded".into(),
            error: None,
        }
    }
    #[test]
    fn immutable_owner_required() {
        let mut r = record("exact", Some(1));
        assert!(owns(&r, 1, "token"));
        assert!(!owns(&r, 2, "token"));
        r.owner_user_id = None;
        assert!(!owns(&r, 1, "token"));
        r.token_fingerprint = Some(format!("{:x}", Sha256::digest(b"token")));
        assert!(owns(&r, 1, "token"));
        assert!(!owns(&r, 1, "new-session"));
    }
    #[test]
    fn no_timestamp_fallback_or_duplicate_identity() {
        let root = std::env::temp_dir().join(uuid::Uuid::new_v4().to_string());
        std::fs::create_dir(&root).unwrap();
        let file = root.join("audio.ogg");
        std::fs::write(&file, b"recording").unwrap();
        let mut r = record("exact", Some(1));
        r.filepath = file.to_string_lossy().into();
        assert!(resolve(&[r.clone()], "different", 1, "token", &root).is_err());
        assert!(resolve(&[r.clone()], "exact", 2, "token", &root).is_err());
        assert_eq!(
            resolve(&[r.clone()], "exact", 1, "token", &root).unwrap(),
            file.canonicalize().unwrap()
        );
        assert!(resolve(&[r.clone(), r], "exact", 1, "token", &root).is_err());
        std::fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn path_must_be_in_recordings_directory() {
        let root = std::env::temp_dir().join(uuid::Uuid::new_v4().to_string());
        std::fs::create_dir(&root).unwrap();
        let outside = root.with_extension("ogg");
        std::fs::write(&outside, b"private").unwrap();
        let mut r = record("exact", Some(1));
        r.filepath = outside.to_string_lossy().into();
        assert!(resolve(&[r], "exact", 1, "token", &root).is_err());
        std::fs::remove_file(outside).unwrap();
        std::fs::remove_dir(root).unwrap();
    }
}
