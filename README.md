# sap-tui — a SAP DIAG terminal viewer

Connect to a SAP **DIAG** dispatcher and render its screens right in your
terminal — no SAP GUI required. Read-only, harmless, one static binary.

## Use

```sh
sap-tui --addr HOST:PORT
# e.g.
sap-tui --addr demo.desude.su:3200
```

Press `q` to quit.

## Install

Grab a prebuilt binary from the [Releases](../../releases) page (Linux / macOS),
`chmod +x`, run. Or build it yourself:

```sh
go build -o sap-tui ./cmd/sap-tui
```

Pure Go, no cgo. MIT licensed. Not affiliated with SAP SE; "SAP" names the
protocol it speaks.
