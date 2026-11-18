<p align="center">
  <img src="assets/banner.png" alt="Remote Shell" width="640">
</p>

Open a terminal on any of your computers from your phone. A small Go agent on each computer exposes real shells, and a mobile app (Ionic Vue + Capacitor, with xterm.js) connects to it over your private [Tailscale](https://tailscale.com) network. There is no central server, no account and no port forwarding: the agent only listens on its Tailscale address.

> **Status: early.** It works end to end and has a broad automated test suite, but it has been used by one person on a few devices. A remote shell is a sensitive tool: read [Security](#security) and [Limitations](#limitations) before you rely on it.

## What it does

- **Real terminals.** Each session is a real shell on a PTY (Unix) or ConPTY (Windows), not simulated command execution.
- **Several terminals at once**, with tabs. Each keeps its own screen when you switch.
- **Persistent and temporary sessions.** Persistent ones keep running when your phone disconnects. Temporary ones close themselves after a period with no connection (30 minutes by default).
- **Reconnection that loses nothing.** If the connection drops (for example when switching between Wi-Fi and mobile data) the app reconnects and resumes from the last output it received. If the output it missed no longer fits in the agent's buffer, it tells you.
- **Pairing by QR code.** Each phone gets its own revocable credential.
- **Runs as an ordinary user**, as a background service, on macOS, Linux and Windows.

## How it works

```text
┌────────────────────────┐
│  Phone (iOS / Android) │
│  Ionic Vue + Capacitor │
│  xterm.js              │
└───────────┬────────────┘
            │  HTTPS / WSS, over Tailscale
     ┌──────┴───────┐
     ▼              ▼
┌──────────┐   ┌──────────┐
│ Computer │   │ Computer │
│ Go agent │   │ Go agent │
│  shells  │   │  shells  │
└──────────┘   └──────────┘
```

Each agent is independent: it owns its shells, its list of authorized phones (in a local SQLite database) and its TLS certificate. Tailscale provides the private network; the agent does its own authentication on top, so being on your tailnet is not enough to get a shell.

## Requirements

- [Tailscale](https://tailscale.com/download) on every computer and phone, with **MagicDNS** and **HTTPS Certificates** enabled in the [admin console](https://login.tailscale.com/admin/dns).
- A computer running macOS, Linux or Windows for the agent.
- To build the agent from source: Go (the version in `agent/go.mod`).
- To build the app: Node.js and npm, plus Xcode (iOS) and/or Android Studio (Android). The app is not published on any store yet, so you build and install it yourself.

## Install the agent

Download the archive for your system from the [releases](../../releases) page, check it against `SHA256SUMS` and unpack `remote-shell` (`remote-shell.exe` on Windows), or build it yourself:

```bash
cd agent
go build -o remote-shell ./cmd/remote-shell
./remote-shell version
```

### 1. Get a TLS certificate for the computer

The phone validates the agent's certificate, so it must be valid for the computer's MagicDNS name (`<computer>.<tailnet>.ts.net`). Tailscale issues one for free:

```bash
tailscale cert <computer>.<tailnet>.ts.net
```

This writes a `.crt` and a `.key` file. Keep the key private and outside any repository. Certificates last about 90 days: run the same command again to renew; the running agent picks up the new files without a restart.

### 2. Check the setup

```bash
./remote-shell doctor -tls-cert <computer>.<tailnet>.ts.net.crt -tls-key <computer>.<tailnet>.ts.net.key
```

It checks that Tailscale is up, the certificate is valid (and for how long), the MagicDNS name resolves to this computer, the key and database are private to your user and, if the agent is running, that it refuses connections on every address except its Tailscale one and that a client validating the certificate accepts the TLS handshake.

### 3. Run it

```bash
./remote-shell -tls-cert <file>.crt -tls-key <file>.key
```

It listens on `https://<tailscale-address>:8443`. It refuses to start as root or administrator, and it will not listen on any address that is not a Tailscale (or loopback) address.

### 4. Keep it running (optional)

```bash
./remote-shell service install -tls-cert <file>.crt -tls-key <file>.key
./remote-shell service status
```

This installs a per-user background service that restarts after a crash and writes its log to `agent.log` in the data directory. It needs no administrator rights:

| System | Mechanism | Starts |
|---|---|---|
| macOS | LaunchAgent | at login |
| Linux | `systemd --user` unit | at login (at boot with `loginctl enable-linger $USER`) |
| Windows | Scheduled task | at logon |

Use `service start`, `stop` and `uninstall` to control it. Pass any agent flag to `service install`.

## Pair your phone

1. On the computer, run `./remote-shell pair`. It shows a QR code valid for 5 minutes and for a single use.
2. In the app, tap **Pair a computer** and scan it (or paste the pairing link).
3. Open the computer and tap **+** to start a terminal.

Manage phones from the computer with `./remote-shell devices list` and `./remote-shell devices revoke <id>`. Removing a computer in the app can revoke that phone on the agent at the same time.

## Build the mobile app

```bash
cd mobile
npm install
npm run build
npx cap sync
```

- **iOS:** open `mobile/ios/App/App.xcodeproj` in Xcode, choose your signing team under *Signing & Capabilities* (it is not stored in the repository) and run it on your device.
- **Android:** open `mobile/android` in Android Studio, or run `./gradlew assembleDebug` there. The minimum is Android 8.0 (API 26).

Use `npm run dev` to work on the web interface in a browser, and `npm test` for the unit tests.

## Configuration

Everything is configured with command-line flags (also accepted by `service install`).

| Flag | Default | Meaning |
|---|---|---|
| `-tls-cert`, `-tls-key` | required | Certificate and private key for HTTPS |
| `-port` | `8443` | HTTPS port |
| `-listen` | Tailscale address | IP to listen on. Only Tailscale or loopback addresses are accepted |
| `-name` | host name | Name shown in the app |
| `-data-dir` | user config dir | Where the database and control socket live |
| `-log-file` | stderr | Write logs to a file (rotated at 10 MiB at startup) |
| `-log-level` | `info` | `debug`, `info`, `warn` or `error` |
| `-shell` | your login shell | Default shell for new terminals |
| `-max-sessions` | `16` | Maximum simultaneous terminals |
| `-temp-session-ttl` | `30m` | Idle time after which a temporary session with no client closes |
| `-session-history` | `24h` | How long ended sessions stay listed in the app |
| `-output-buffer-kb` | `1024` | Output kept per terminal for reconnection |
| `-pairing-ttl` | `5m` | Validity of a pairing QR code |
| `-auth-max-failures`, `-auth-lockout` | `10`, `5m` | Failed attempts per minute before a client is locked out, and for how long |
| `-ops-per-minute` | `30` | Rate limit for tickets, new sessions and terminal connections |
| `-cols`, `-rows` | `120`, `40` | Default terminal size |

## Security

A remote shell is a high-value target, so the design is conservative.

- **Private network only.** The agent binds to the Tailscale address and refuses wildcard and public addresses. Nothing needs to be exposed to the internet.
- **Mandatory TLS** with certificates the phone validates normally. There is no plain HTTP mode and no option to skip verification.
- **Explicit pairing.** A pairing code has at least 128 bits of entropy, works once and expires. Each phone gets its own random token, stored hashed on the agent and in the Keychain / Keystore on the phone.
- **Revocation** is immediate: the token, its pending connection tickets and its open terminals stop working.
- **Short-lived, single-use tickets** open each terminal WebSocket, so the phone's token never appears in a URL.
- **Abuse limits:** lockouts after repeated failures, rate limits, and bounds on request sizes, sessions, connections and buffers.
- **Least privilege:** the agent refuses to run as root or administrator and never elevates. Clients can only pick a shell from the agent's own list, never a path.
- **No secrets in logs:** tokens, pairing codes, tickets and terminal contents are never logged.

Please report vulnerabilities privately rather than in a public issue.

## Limitations

- Terminals are independent background shells. They do **not** mirror a terminal window you already have open on the computer, and they do not survive a restart of the agent or the computer (they are listed as ended afterwards).
- The agent trusts every paired phone completely: any paired phone can act as you on that computer, and can revoke other phones. There are no per-device permissions.
- No file transfer, session sharing between people, command history or push notifications.
- Tested mainly on macOS with an iPhone. Linux and Windows are covered by automated tests in CI but have had little real-world use, and Android has been tried on an emulator only.
- The app is not on any store, and there is no signed release of it.

## Development

```bash
cd agent && go test -race ./...          # agent tests
cd mobile && npm test && npm run lint    # app tests and lint
RS_SOAK_SECONDS=300 go test -run TestSoak -v ./internal/server   # long-running check (from agent/)
scripts/build-release.sh 1.2.3           # release archives for every platform
```

The repository is organised as:

```text
agent/    Go agent: HTTPS API, WebSocket terminals, pairing, storage, CLI
mobile/   Ionic Vue + Capacitor app (iOS and Android projects included)
scripts/  Release build script
```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
