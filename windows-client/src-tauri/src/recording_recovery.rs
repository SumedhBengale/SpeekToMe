//! Recover only complete, checksum-valid Ogg pages after an interrupted write.
use std::{
    fs::{File, OpenOptions},
    io::{Read, Write},
    path::Path,
};
pub fn atomic_write(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let temporary = path.with_extension("atomic-tmp");
    let mut options = OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    let mut file = options.open(&temporary)?;
    file.write_all(data)?;
    file.sync_all()?;
    drop(file);
    crate::meeting_upload::replace_file(&temporary, path)?;
    #[cfg(unix)]
    {
        File::open(path.parent().ok_or(std::io::ErrorKind::NotFound)?)?.sync_all()?;
    }
    Ok(())
}
fn crc(bytes: &[u8]) -> u32 {
    let mut value = 0u32;
    for &byte in bytes {
        value ^= (byte as u32) << 24;
        for _ in 0..8 {
            value = if value & 0x80000000 != 0 {
                (value << 1) ^ 0x04c11db7
            } else {
                value << 1
            };
        }
    }
    value
}
pub fn repair(path: &Path) -> Result<(), String> {
    let mut file = File::open(path).map_err(|_| "Recorded audio is missing")?;
    let mut valid = 0u64;
    let mut pages = 0;
    loop {
        let mut header = [0u8; 27];
        if file.read_exact(&mut header).is_err() {
            break;
        }
        if &header[..4] != b"OggS" || header[4] != 0 {
            break;
        }
        let expected = u32::from_le_bytes(header[22..26].try_into().unwrap());
        header[22..26].fill(0);
        let mut page = header.to_vec();
        let mut lacing = vec![0; header[26] as usize];
        if file.read_exact(&mut lacing).is_err() {
            break;
        }
        let length: usize = lacing.iter().map(|&v| v as usize).sum();
        page.extend_from_slice(&lacing);
        let start = page.len();
        page.resize(start + length, 0);
        if file.read_exact(&mut page[start..]).is_err() || crc(&page) != expected {
            break;
        }
        if pages == 0 && !page[start..].starts_with(b"OpusHead") {
            return Err("Recording format is invalid".into());
        }
        valid += page.len() as u64;
        pages += 1;
    }
    if pages < 3 {
        return Err(
            "No complete recorded audio could be recovered. Local files have been preserved."
                .into(),
        );
    }
    let file = OpenOptions::new()
        .write(true)
        .open(path)
        .map_err(|_| "Cannot repair recording")?;
    file.set_len(valid).map_err(|_| "Cannot repair recording")?;
    file.sync_all()
        .map_err(|_| "Cannot save repaired recording")?;
    Ok(())
}
pub fn repair_meeting(path: &Path) -> Result<(), String> {
    for file in [
        path.to_path_buf(),
        path.with_extension("mic.ogg"),
        path.with_extension("loopback.ogg"),
    ] {
        repair(&file)?;
    }
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn interrupted_page_is_trimmed_without_losing_complete_audio() {
        let path = std::env::temp_dir().join(format!("recovery-{}.ogg", uuid::Uuid::new_v4()));
        let bytes = crate::audio_processing::encode_opus_ogg(&vec![1000; 16000], 16000, 1).unwrap();
        let mut torn = bytes.clone();
        torn.extend_from_slice(b"OggS\0broken");
        std::fs::write(&path, torn).unwrap();
        repair(&path).unwrap();
        assert_eq!(std::fs::read(&path).unwrap(), bytes);
        std::fs::remove_file(path).unwrap();
    }
    #[test]
    fn corrupted_audio_is_not_uploaded_or_deleted() {
        let path = std::env::temp_dir().join(format!("recovery-{}.ogg", uuid::Uuid::new_v4()));
        std::fs::write(&path, b"private broken audio").unwrap();
        assert!(repair(&path).is_err());
        assert_eq!(std::fs::read(&path).unwrap(), b"private broken audio");
        std::fs::remove_file(path).unwrap();
    }
}

#[cfg(test)]
mod torn_page_tests {
    use super::*;
    #[test]
    fn power_loss_in_audio_page_preserves_earlier_complete_packets() {
        let path = std::env::temp_dir().join(format!("torn-audio-{}.ogg", uuid::Uuid::new_v4()));
        let source =
            crate::audio_processing::encode_opus_ogg(&vec![1000; 16000], 16000, 1).unwrap();
        let mut reader = ogg::reading::PacketReader::new(std::io::Cursor::new(source));
        let mut bytes = Vec::new();
        {
            let mut writer = ogg::writing::PacketWriter::new(&mut bytes);
            while let Some(packet) = reader.read_packet().unwrap() {
                let granule = packet.absgp_page();
                writer
                    .write_packet(
                        packet.data.into_boxed_slice(),
                        1,
                        ogg::writing::PacketWriteEndInfo::EndPage,
                        granule,
                    )
                    .unwrap();
            }
        }
        let mut offset = 0;
        let mut last = 0;
        while offset < bytes.len() {
            last = offset;
            let segments = bytes[offset + 26] as usize;
            let size: usize = bytes[offset + 27..offset + 27 + segments]
                .iter()
                .map(|&x| x as usize)
                .sum();
            offset += 27 + segments + size;
        }
        let expected = bytes[..last].to_vec();
        bytes.truncate(bytes.len() - 1);
        std::fs::write(&path, bytes).unwrap();
        repair(&path).unwrap();
        assert_eq!(std::fs::read(&path).unwrap(), expected);
        std::fs::remove_file(path).unwrap();
    }
}
