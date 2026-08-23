# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in GoPilotKit, please report it responsibly:

1. **Do NOT** open a public GitHub issue for security vulnerabilities
2. **Email** the maintainer directly at [INSERT SECURITY EMAIL]
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

## Response Timeline

- **Acknowledgment**: Within 48 hours
- **Initial assessment**: Within 1 week
- **Fix timeline**: Depends on severity, typically 1-2 weeks

## Security Considerations

GoPilotKit is designed with security in mind:

- **Zero CGO** — No C dependencies, no memory safety issues from native code
- **Single binary** — Minimal attack surface
- **PII scrubbing** — Stream-level masking before external API calls
- **Config secrets** — API keys use `${ENV_VAR}` syntax, never stored in plaintext
- **WASM sandboxing** — Plugins run in isolated WASM runtime (wazero)
- **Circuit breaker** — Prevents cascade failures on provider errors
- **Enterprise features** — SSO/SAML, RBAC, audit logging (enterprise profile)
