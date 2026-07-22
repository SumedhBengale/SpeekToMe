# SpeekTo.Me Helper Tools

## Admin TOTP Encryption Helper (`encrypt_totp.go`)

This script encrypts a raw Base32 TOTP secret (e.g. from Google Authenticator, Authy, 1Password) using the server's `ENCRYPTION_KEY` (`AES-256-GCM`), and outputs a ready-to-run SQL `UPDATE` statement to set the `totp_secret` and grant `is_admin = 1` for the specified user in the database.

### Usage

Run from the project root directory:

```bash
go run helpers/encrypt_totp.go -secret YOUR_RAW_TOTP_SECRET -email admin@speekto.me
```

Or interactively prompt for the secret:

```bash
go run helpers/encrypt_totp.go
```

### Options

- `-secret <string>`: Raw Base32 TOTP secret string.
- `-email <string>`: Target admin user email address for the SQL template (default: `your_admin_email@example.com`).

### Example Output

```text
============================================================
 SpeekTo.Me Admin TOTP Encryption Helper
============================================================
[+] Raw TOTP Secret:  R3K7WJQ8MZ5VNX2YHLC4AETP9FGB6UDS
[+] Key Source:       ENCRYPTION_KEY environment variable
[+] Encrypted Secret: 880f764817d6829f14ba4c52a44aaa53...

[+] Copy & Paste SQL Query to update DB:
UPDATE users SET totp_secret = '880f764817d6829f14ba4c52a44aaa53...', is_admin = 1 WHERE email = 'admin@speekto.me';
============================================================
```
