# Email and secret scope

- `APP_URL` is the only trusted origin for magic/verification/reset links;
  `r.Host` is never used. Malformed/HTTP-in-production values fail boot.
- `SMTP_ADDR`/`SMTP_FROM` are non-secret project settings. `SMTP_USERNAME`/
  `SMTP_PASSWORD` belong in the age-encrypted `~/.secrets/<APP_NAME>.env.age`
  (created by `bin/init-secrets`), never in the repo, a test fixture or chat.
- The secrets file is scoped by `APP_NAME`. A generated project MUST be
  renamed (`make rename` / `gogogo new`) before boot; running a second project
  as `APP_NAME=gogogo` on a host with the template's secrets would load that
  file. Never copy another project's `.env.age`. Name scoping is not a sandbox:
  same-user code can read every file in `~/.secrets`, so isolate untrusted
  projects with a separate OS user or container.
- Empty SMTP config is the default and leaves magic/verification/reset closed
  (503). Local capture: `make mailpit` (`SMTP_ADDR=127.0.0.1:1025`,
  `SMTP_FROM=dev@example.test`). Real delivery: provider SMTP (Resend, Brevo,
  SendGrid, SES, ...) with SPF/DKIM/DMARC.
- The MCP/CLI `new` handoff prints the email setup; agents may set non-secret
  `APP_URL`/`SMTP_ADDR`/`SMTP_FROM`, but must ask the human to place the SMTP
  password in the secrets file.
