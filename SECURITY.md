# Security Policy

## Supported Versions

Security fixes are applied to the latest released maply version. Before
`v1.0.0`, upgrading to the newest pre-release or stable tag may be required.

## Reporting A Vulnerability

Please do not open a public issue for a suspected vulnerability or include
credentials, database dumps, Telegram identifiers, tokens, or production logs in
an issue.

A way to learn today's place before the reveal counts as a vulnerability: an
API response, an image name or its metadata, a log line, a timing difference.
Report it privately, the same as a leaked token.

Use GitHub's private vulnerability reporting for `FreshLabDev/maply`. If it is
not available, contact an Asterfield maintainer privately through an existing
trusted channel and include only the minimum reproduction details needed to
investigate.

We will acknowledge a report, assess affected versions, coordinate a fix, and
publish an advisory when disclosure is safe.

## Operator Responsibilities

- Keep `.env`, PostgreSQL dumps, the image volume and `curation/` outside Git.
  Curation batches hold source IDs and coordinates of future places.
- Use a unique strong password for the `maply_core` role.
- Do not publish the PostgreSQL port to the internet. The service is reached
  only through the Cloudflare Tunnel.
- Back up the database and the image volume together before applying a release
  with new migrations.
