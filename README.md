# sap-tui — a SAP DIAG terminal viewer

Connect to a SAP **DIAG** dispatcher and render its screens right in your
terminal — no SAP GUI required. Read-only, harmless, one static binary.

## Try it live 👀

```sh
sap-tui demo.desude.su:3200
```

There's a little something running there. Point your SAP GUI at the same host
(instance **00**) if you have one — or just watch it here. `q` quits.

## Use

```sh
sap-tui HOST:PORT          # positional
sap-tui --addr HOST:PORT   # or the flag
```

## Install

Grab a prebuilt binary from the [Releases](../../releases/latest) page
(Linux / macOS / Windows), `chmod +x`, run:

```sh
curl -L -o sap-tui https://github.com/oisee/sap-tui/releases/latest/download/sap-tui-macos-arm64
chmod +x sap-tui
./sap-tui demo.desude.su:3200
```

On macOS, if Gatekeeper blocks the unsigned binary: `xattr -d com.apple.quarantine ./sap-tui`.

Or with Go:

```sh
go install github.com/oisee/sap-tui/cmd/sap-tui@latest
```

Or build it yourself: `go build -o sap-tui ./cmd/sap-tui`.

## What's on the other end

The server that generates those screens is **sap-lsd** — a rogue SAP GUI DIAG
server: **https://github.com/oisee/sap-lsd**. `sap-tui` is just the terminal
front-end for it.

Both are built on **[open-diag-go](https://github.com/oisee/open-diag-go)** — the
pure-Go SAP **DIAG** protocol library (read the wire, describe screens, draw them
on a real SAP GUI) — which in turn uses **[open-rfc-go](https://github.com/oisee/open-rfc-go)**
for the NI network transport.

---

Pure Go, no cgo. MIT licensed. Not affiliated with SAP SE; "SAP" names the
protocol this speaks.
