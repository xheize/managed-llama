# Security

Managed Llama is a Windows-only local management application. The gateway
accepts loopback bind addresses and loopback/localhost HTTP Host values only.
Do not expose the dashboard through a tunnel or reverse proxy to other users.
Ordinary user mode has no API authentication; other local processes can control it.

Windows service and elevated modes require a separate management key. Its holder
can control privileged processes. Keep the key restricted to Administrators and
SYSTEM. The ordinary login tray does not read it; authenticate in the dashboard.
Hugging Face tokens are Base64-encoded on disk, not encrypted.

Report vulnerabilities through the repository's private vulnerability reporting
feature when the maintainer has enabled it. Do not put credentials or a working
exploit against someone else's system in a public issue. Until a private channel
is available, request one in an issue without sensitive details.

Only the latest release is maintained. Administrator installation, rollback and
uninstall require separate Windows lifecycle validation; unit tests do not prove
those operations work on every supported Windows configuration.
