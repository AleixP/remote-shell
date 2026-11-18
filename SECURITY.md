# Security policy

Remote Shell gives access to a shell on your computers, so security reports are taken seriously.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Use GitHub's private reporting instead: open the repository's **Security** tab and choose **Report a vulnerability**. Describe what you found, how to reproduce it and what an attacker could do with it.

You can expect an acknowledgement within a few days. This is a small project maintained by one person, so fixes depend on availability, but reports are handled before any new feature.

## What is in scope

- Anything that lets someone who is not a paired device run commands, read terminal output or learn secrets.
- Bypasses of device pairing, revocation, connection tickets, rate limits or the Tailscale-only binding.
- Leaks of tokens, pairing codes, tickets or terminal contents in logs, errors or storage.
- Weaknesses in how the app stores credentials or validates the agent's TLS certificate.

## Supported versions

Only the latest release and the `main` branch receive fixes.

## Design notes

The security model (pairing, tokens, tickets, TLS, rate limiting, least privilege) is summarized in the [README](README.md#security). The agent trusts every paired phone completely; a compromised paired phone is equivalent to a compromised account on that computer, which is why individual phones can be revoked.
