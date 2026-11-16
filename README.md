# Remote\-Shell

A self\-hosted remote terminal solution that lets you access and manage terminal sessions on your computers directly from your mobile device\.

The project consists of a Go agent installed on each computer and a mobile application built with Ionic Vue and Capacitor\. Communication takes place over a private Tailscale network, with no central backend required\.

## Features

- **Remote terminal access** — Connect to your computers from iOS and Android\.
- **Cross\-platform agent** — Support for Windows, macOS and Linux\.
- **Multiple terminal sessions** — Create and manage independent terminal sessions on each computer\.
- **Persistent sessions** — Keep terminal processes running when the mobile client disconnects\.
- **Session recovery** — Reconnect to existing sessions and recover available terminal output\.
- **Secure device pairing** — Register mobile devices using a QR code\.
- **Device management** — Authorize and revoke individual mobile devices\.
- **Private connectivity** — Use Tailscale to connect devices without exposing the agent to the public internet\.
- **No central backend** — Each computer runs and manages its own agent\.

## Architecture

```text
┌──────────────────────────────┐
│       Mobile Application     │
│                              │
│       Ionic Vue              │
│       Capacitor              │
│       xterm.js               │
└──────────────┬───────────────┘
               │
               │ HTTPS / WebSocket
               │
        Tailscale Network
               │
       ┌───────┴────────┐
       │                │
       ▼                ▼
┌──────────────┐ ┌──────────────┐
│  Computer A  │ │  Computer B  │
│              │ │              │
│  Go Agent    │ │  Go Agent    │
│              │ │              │
│  Terminal 1  │ │  Terminal 1  │
│  Terminal 2  │ │  Terminal 2  │
│  Terminal 3  │ │              │
└──────────────┘ └──────────────┘
```

Each agent operates independently and manages its own configuration, authorized devices and terminal sessions\.

The mobile application communicates directly with the selected agent over the private network\.

## Technology Stack

### Agent

- Go
- SQLite
- PTY / ConPTY for interactive terminal processes
- HTTP API
- WebSocket
- TLS

### Mobile Application

- Ionic Vue
- Capacitor
- TypeScript
- xterm\.js
- xterm\.js Fit Addon

### Networking

- Tailscale
- HTTPS
- Secure WebSocket &#40;WSS&#41;

## Project Structure

```text
remote-shell/
├── agent/
│   ├── cmd/
│   │   └── remote-shell/
│   │       └── main.go
│   ├── internal/
│   │   ├── server/
│   │   ├── terminal/
│   │   ├── auth/
│   │   ├── config/
│   │   └── storage/
│   ├── migrations/
│   ├── go.mod
│   └── README.md
│
├── mobile/
│   ├── src/
│   │   ├── components/
│   │   ├── views/
│   │   ├── services/
│   │   ├── stores/
│   │   ├── composables/
│   │   └── types/
│   ├── capacitor.config.ts
│   └── package.json
│
└── README.md
```

## Requirements

Before running the project, make sure you have the following installed:

- Go &#40;version defined by the agent module&#41;\.
- Node\.js and npm\.
- Tailscale installed and configured on the computers and mobile devices\.
- Xcode for iOS development\.
- Android Studio and Android SDK for Android development\.

The agent and mobile application are developed and run independently\.

## Getting Started

### 1\. Clone the repository

```bash
git clone <repository-url>
cd remote-shell
```

### 2\. Run the Go agent

Navigate to the agent directory:

```bash
cd agent
```

Download dependencies:

```bash
go mod download
```

Run the agent:

```bash
go run ./cmd/remote-shell
```

The agent requires a valid configuration and access to the Tailscale network\.

### 3\. Run the mobile application

Navigate to the mobile directory:

```bash
cd mobile
```

Install dependencies:

```bash
npm install
```

Start the development server:

```bash
npm run dev
```

For native development, use the appropriate Capacitor commands to open the iOS or Android project\.

## Security

Security is an essential part of the project\.

- Agents are accessible only through the private Tailscale network\.
- All application communication uses HTTPS and WSS\.
- TLS certificates must be validated by clients\.
- Mobile devices must be explicitly paired and authorized\.
- Each authorized device receives its own access credentials\.
- Credentials must be stored securely on mobile devices\.
- Individual devices can be revoked\.
- WebSocket connections use short\-lived, single\-use connection tickets\.
- Agents run with normal user permissions rather than administrator or root privileges\.

Tailscale provides private network connectivity, but application\-level authentication and authorization are handled by the agent itself\.

## Supported Platforms

|Component         |Platforms                                             |
|------------------|------------------------------------------------------|
|Go Agent          |Windows, macOS, Linux                                 |
|Mobile Application|iOS, Android                                          |
|Terminal          |PowerShell, CMD, Zsh, Bash and other configured shells|
|Network           |Tailscale                                             |

## Current Scope

The initial release focuses on reliable remote terminal access\.

The following capabilities are outside the initial scope:

- Restoring terminal processes after a computer restart\.
- Sharing terminal sessions between users\.
- File transfers\.
- Command history and session recording\.
- Push notifications\.
- Centralized backend services\.
- Built\-in Tailscale installation and management\.
- Remote desktop or graphical screen sharing\.

## License

License not yet specified\.