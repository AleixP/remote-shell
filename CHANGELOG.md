# Changelog

All notable changes to this project are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). Versions below 1.0.0 are early: anything can still change.

## [0.1.0] - 2026-11-21

The first public release. It works end to end and has a broad automated test suite, but it has been used by one person on a few devices, mostly a Mac with an iPhone. Read [Security](README.md#security) and [Limitations](README.md#limitations) before relying on it.

### Agent (macOS, Linux and Windows)

- Real shells on a PTY (Unix) or ConPTY (Windows), several at once, persistent or temporary.
- Listens only on the computer's Tailscale address, over HTTPS with a certificate from `tailscale cert` that is reloaded without a restart.
- Pairing with a one-time QR code (`remote-shell pair`, or `pair -link` to print a link instead), a separate revocable credential for each phone (`devices list` and `devices revoke`), and single-use tickets to open each terminal.
- Reconnection that resumes from the last output received, with a bounded output buffer per terminal and a list of recently ended terminals.
- Rate limits and lockouts, bounded input everywhere, a shell allowlist, no secrets in logs, and a refusal to run as root or administrator.
- `remote-shell doctor` checks Tailscale, the certificate, the MagicDNS name, file permissions, that nothing answers outside Tailscale, and the TLS handshake.
- `remote-shell service install` runs it as a background service for your user: a LaunchAgent, a `systemd --user` unit or a scheduled task. No administrator rights.

### Mobile app (iOS and Android)

- Ionic Vue and Capacitor, with xterm.js terminals, several open as tabs, and a row of keys a phone keyboard lacks.
- Copy and paste, with selection by finger: press and hold, or the Select key; a handle at each end and a menu over the selection.
- Rename computers and terminals on the phone.
- Reconnects at once when the phone regains connectivity, and keeps the launch screen up until the interface is ready.
- Credentials in the Keychain (iOS) or Keystore (Android).

### Checks

- CI on Linux, macOS and Windows, plus `govulncheck`, `npm audit`, `gosec`, CodeQL and weekly fuzz tests of the parsers, authentication and the API.
- Stress and soak tests of the agent: concurrent terminals, abrupt disconnects, slow clients and a long run.

### Downloads

Archives of the agent for macOS (Intel and Apple silicon), Linux (amd64 and arm64) and Windows (amd64), with `SHA256SUMS`. Each file carries a build provenance attestation: see [Install the agent](README.md#install-the-agent) to verify what you download.

The mobile app is not published on any store and there is no signed build of it: [build it yourself](README.md#build-the-mobile-app).

### Known limitations

- Terminals are independent background shells: they do not mirror a terminal window you already have open, and they do not survive restarting the agent or the computer.
- Any paired phone can act as you on that computer, and can revoke other phones. There are no per-device permissions.
- Linux, Windows and Android have had little real-world use beyond the automated tests and an emulator.
- No independent security review.

[0.1.0]: https://github.com/AleixP/remote-shell/releases/tag/v0.1.0
