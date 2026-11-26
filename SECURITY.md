# Security Policy

## Reporting a vulnerability

Please report security issues privately via GitHub Security Advisories on
the NeoRouter repository, or by email to the NeoLabs maintainers. Do not open
public issues for vulnerabilities.

Include:

- A description of the issue and its impact.
- Steps to reproduce or a proof of concept.
- The affected component (gateway `/v1`, management API `/api/v1`, CLI, Flutter client).

## Scope

In scope:

- Authentication or authorization bypass (sessions, passkeys, QR sign-in,
  password reset, router keys, admin routes).
- Getting an install to accept an update or download that was not signed by the
  release key.
- Access to another user's provider accounts, keys, catalog or request logs.
- Disclosure of provider API keys (they are stored AES-256-GCM encrypted with
  `~/.neorouter/data/secret.key`) or router key secrets (stored only as SHA-256 hashes).
- Budget or free-only bypass that lets a router key spend money it should not.
- Server-side request forgery through provider base URLs beyond what an
  authenticated user can already configure for their own account.

## Design notes

- NeoRouter never stores prompts or completions. Request logs contain metadata only.
- Provider accounts belong to one user; their keys are never shown to anyone.
- Updates are verified against an Ed25519 key built into the binary.
- Custom base URLs are user-supplied by design. Run NeoRouter on a network
  where that is acceptable, or restrict egress at the network level.
