# Security Policy

MicroJet ships middleware that sits on the security boundary of the services that
use it — JWT verification, rate limiting, CORS, request logging, TLS termination —
so its vulnerabilities are its users' vulnerabilities. We take reports seriously
and aim to respond quickly.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.** Public issues are
visible to everyone, including before a fix exists.

Report privately through either channel:

- **GitHub private vulnerability reporting** (preferred) — open the
  [Security tab](https://github.com/hatami57/microjet/security) of the repository
  and choose **Report a vulnerability**. This keeps the report, the discussion,
  and the eventual advisory in one place.
- **Email** — <software.apan@gmail.com>. Put `SECURITY` in the subject line. If
  you want to encrypt the report, ask in a first plain-text message and we will
  arrange a key.

A useful report includes:

- the affected module(s) and version (e.g. `httpx v0.30.0`),
- a description of the issue and its impact,
- the minimal steps, configuration, or proof-of-concept needed to reproduce it,
- any suggested remediation, if you have one.

## What to expect

- **Acknowledgement** within **3 business days** that the report was received.
- An initial **assessment** (severity, whether we can reproduce it, and a rough
  remediation plan) within **10 business days**.
- Regular updates as the fix progresses. We will let you know when a release
  containing the fix is available.
- **Credit** in the advisory and release notes for the reporter, unless you ask
  to remain anonymous.

We ask that you give us a reasonable window to release a fix before any public
disclosure, and that testing stays within systems you own or are authorized to
test — no denial-of-service, data exfiltration, or attacks against third parties.
Good-faith research conducted under this policy is welcome.

## Supported versions

MicroJet is a multi-module monorepo released **in lockstep on a single version**
(`core/vX.Y.Z`, `httpx/vX.Y.Z`, …; see
[docs/compatibility.md](docs/compatibility.md)). Security fixes are shipped in a
new release on top of the **latest released minor**; there are no long-term
maintenance branches for older minors.

| Version           | Supported          |
| ----------------- | ------------------ |
| Latest minor      | :white_check_mark: |
| Any earlier minor | :x:                |

While the project is pre-1.0 (`v0.x`), the "latest minor" is simply the most
recent released version across all modules. If you are affected by an issue,
upgrade to the release that contains the fix; back-ports to older versions are
handled only by exception and by prior arrangement.
