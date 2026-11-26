# Security Policy

## Supported Versions

Security fixes are applied to the latest stable release and, when affected, the
current beta line. Older releases should be updated before a report is
validated.

## Report a Vulnerability Privately

Do not open a public issue for a suspected vulnerability.

Use GitHub's
[private vulnerability reporting](https://github.com/NeoLabs-Systems/NeoRouter/security/advisories/new)
to send the report to the maintainer.

Include:

- The affected version or commit.
- The affected component (gateway, management API, CLI or dashboard).
- Reproduction steps or a minimal proof of concept.
- The impact you observed.
- Any known mitigations.

Do not include live credentials, personal data, or access to a production
system. Use test accounts and redact logs.

The maintainer will confirm receipt, investigate the report, coordinate a fix,
and credit the reporter when requested and appropriate. Please allow time for a
release before publishing technical details.

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
