# Release notes

`v<version>.md` here becomes the body of that release. `Cut a release` reads
`doc/release-notes/v$APP_VERSION.md` if it exists and falls back to GitHub's
generated commit list if it does not — which answers "what changed" but not
"should I install this", and the second question is the one a person opening a
release page is asking.

Write it for someone who runs Viritin, not for someone who reads the diff: what
they get, what they have to set to get it, and what upgrading costs them.
