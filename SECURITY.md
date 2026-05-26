# Security Policy

[日本語](SECURITY.ja.md)

## Supported versions

Vantyx is in pre-1.0 development. Only the latest tagged release on `main`
receives security fixes. Older `0.y` versions are not maintained.

| Version | Supported          |
|---------|--------------------|
| `0.y.z` (latest) | :white_check_mark: |
| any older        | :x:                |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security problems.** Please
use the private channel below so we can investigate and ship a fix before
public disclosure.

- **GitHub private advisory:**
  <https://github.com/nullpo7z/vantyx/security/advisories/new>

When reporting, please include:

- A description of the vulnerability and the affected component
  (HTTP API path, package, file).
- A minimal proof-of-concept or reproduction steps.
- The Vantyx version or commit you tested against.
- Your environment (OS, Go version, browser if relevant).
- An optional CVSS estimate or impact analysis.

We will acknowledge your report within **3 business days** and aim to:

- Provide an initial assessment within **7 business days**.
- Ship a patched release within **30 days** for high-severity issues.

Coordinated disclosure is welcome. We will credit reporters in the release
notes unless they request to remain anonymous.

## Scope

In scope:

- Code in this repository (`cmd/`, `internal/`, `web/src/`).
- The official Docker image built from this repository.
- Documentation that, if followed, leads users into an insecure
  configuration.

Out of scope:

- Vulnerabilities in third-party dependencies should be reported upstream;
  please notify us afterwards so we can pin or patch as needed.
- Misconfiguration by an operator (for example, running with
  `VANTYX_DISABLE_ORIGIN_CHECK=1` on a public deployment).

## Hardening guide

Vantyx is designed to meet **OWASP ASVS Level 2** for sensitive data
storage. See [docs/SECURITY-ASVS-L2.md](docs/SECURITY-ASVS-L2.md) for the
controls implemented and the recommended deployment hardening checklist.
