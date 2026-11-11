#!/bin/bash
# install-browser.sh — install the Chromium the browser gates drive, and prove
# it launches.
#
# WHY THIS EXISTS. The step used to be a single
# `npx playwright install --with-deps chromium`, and `--with-deps` apt-installs
# Playwright's system dependencies. On 2026-11-11 the GitHub runners stopped
# resolving the Ubuntu mirrors (archive.ubuntu.com, security.ubuntu.com,
# azure.archive.ubuntu.com — "Temporary failure resolving") while every other
# host resolved fine (packages.microsoft.com, proxy.golang.org, the npm
# registry, github.com). Three CI runs of PR #399 died in "Failed to install
# browsers" with nothing but that DNS error, while every gate that carries the
# product stayed green (checks, race x3, prod-image): the build was red for a
# reason that had nothing to do with the change under test, and re-running it
# reproduced the same wall.
#
# The dependency install is a convenience, not a requirement: the runner image
# already ships Google Chrome, and therefore the shared libraries Playwright's
# Chromium links against. So the order here is deliberate:
#
#   1. the browser binary (from the Playwright CDN or the restored cache) —
#      the part the gates actually need, and it never touches apt;
#   2. the system dependencies (apt) — best effort, time-boxed, and a loud
#      warning when it fails: the environment is then the image's copy, not
#      the canonical one, and that must be visible in the log;
#   3. the proof — the browser must LAUNCH. Every install step above can
#      "succeed" while the binary cannot start (a missing library, a sandbox
#      the runner forbids), and the suites downstream would fail with a message
#      that names neither the cause nor the fix.
#
# Usage (CI): bash bin/install-browser.sh
#   PLAYWRIGHT_INSTALL_TIMEOUT=300  seconds allowed for the apt phase
#   PLAYWRIGHT_SKIP_DEPS=1          skip the apt phase entirely
# Exit: 0 = the browser is installed and launches. 1 = it does not.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

install_timeout="${PLAYWRIGHT_INSTALL_TIMEOUT:-300}"
skip_deps="${PLAYWRIGHT_SKIP_DEPS:-0}"
# The OS the apt phase is attempted on. Overridable so the contract test can
# exercise the Linux path from a Mac (bin/install-browser_test.sh).
host_os="${PLAYWRIGHT_OS:-$(uname -s)}"

log() { printf '%s\n' "$*"; }

run_bounded() { # run_bounded <seconds> <cmd...>
	local secs="$1"
	shift
	if command -v timeout >/dev/null 2>&1; then
		timeout "$secs" "$@"
	else
		"$@"
	fi
}

# launch_browser proves the browser starts. The first attempt is the sandboxed
# launch (what site-layout.test.mjs does); the second drops the sandbox (what
# the smoke suite does), so a runner that forbids the sandbox is reported as
# what it is instead of failing the gates later.
launch_browser() { # launch_browser <no-sandbox: 0|1>
	local no_sandbox="$1"
	PW_NO_SANDBOX="$no_sandbox" node -e '
		const { chromium } = require("playwright");
		const args = process.env.PW_NO_SANDBOX === "1" ? ["--no-sandbox"] : [];
		chromium
			.launch({ args })
			.then(async (browser) => {
				console.log("chromium " + browser.version() + " launches");
				await browser.close();
			})
			.catch((err) => {
				console.error("chromium does not launch: " + (err && err.message ? err.message : String(err)));
				process.exit(1);
			});
	'
}

log "-> installing the Chromium binary (Playwright CDN or the restored cache)"
if ! npx playwright install chromium; then
	log "ERROR: could not install the Chromium binary"
	exit 1
fi

deps_ok=1
if [ "$skip_deps" = "1" ]; then
	log "-> skipping the system dependencies (PLAYWRIGHT_SKIP_DEPS=1)"
	deps_ok=0
elif [ "$host_os" != "Linux" ]; then
	log "-> skipping the system dependencies (not Linux: there is no apt here)"
	deps_ok=0
else
	log "-> installing the system dependencies (apt; best effort)"
	if run_bounded "$install_timeout" npx playwright install-deps chromium; then
		log "OK: system dependencies installed"
	else
		deps_ok=0
		log "WARNING: could not install the browser system dependencies."
		log "         The runner image already carries the shared libraries"
		log "         Playwright's Chromium links against, so the gates continue"
		log "         on the image's copy. A genuinely missing library fails the"
		log "         launch check below, not a suite."
	fi
fi

log "-> proving the browser launches"
if launch_browser 0; then
	log "OK: browser ready"
	exit 0
fi

if [ "$deps_ok" -eq 0 ]; then
	log "-> retrying the launch after a dependency install"
	if run_bounded "$install_timeout" npx playwright install-deps chromium; then
		if launch_browser 0; then
			log "OK: browser ready (after the dependency install)"
			exit 0
		fi
	fi
fi

if launch_browser 1; then
	log "WARNING: the browser only launches without the sandbox. The suite that"
	log "         does not pass --no-sandbox (site-layout) may still fail."
	exit 0
fi

log "ERROR: no usable Chromium — it could not be installed, or it cannot launch"
exit 1
