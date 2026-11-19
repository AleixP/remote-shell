# Contributing

Thanks for your interest. Remote Shell is a small project, so the process is light.

## Before you start

- For anything bigger than a small fix, open an issue first so we agree on the approach.
- **Security problems go through private reporting**, not issues: see [SECURITY.md](SECURITY.md).
- Read the [README](README.md): in particular *Security* and *Limitations*. Some things are intentionally out of scope: file transfer, session sharing between people, per-device permissions and restoring processes after a restart.

## Rules the code follows

- No central backend: each computer runs its own independent agent.
- Real PTY (Unix) and ConPTY (Windows), never simulated command execution. Operating-system specific code stays isolated per platform.
- The agent only listens on the Tailscale address (or loopback). TLS is mandatory and certificate verification is never disabled.
- Tokens, pairing codes, tickets and terminal input or output are never logged. Errors sent to clients do not reveal paths or internals.
- All input is validated, and buffers, sessions and message sizes are bounded.
- It must work on macOS, Linux and Windows (and iOS and Android for the app).

## Development

```bash
make help        # every agent command
make check       # formatting, go vet (including Windows) and race tests
make fuzz        # fuzz the parsers, authentication and the API with random input
make gosec       # common security mistakes in the Go code
make vuln        # known vulnerabilities in the Go code (govulncheck)
cd mobile && npm ci && npm test && npm run lint
```

Tests are expected with every change: new behavior gets a test, and a bug fix gets a test that fails without the fix. Changes to the agent should keep passing on all three operating systems; CI runs them.

## Pull requests

- Keep them focused: one change per pull request.
- Keep commits small, with short messages in English.
- Fill in the pull request template, including the *Security* section when it applies.
- Do not commit secrets, certificates, `.env` files or personal signing settings. In particular, your Apple Team ID in the Xcode project must stay out of commits.

By contributing you agree that your work is released under the [MIT License](LICENSE).
