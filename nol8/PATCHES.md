# nol8 changes to upstream quic-go

`master` is the upstream release in [`UPSTREAM`](UPSTREAM) (commit in
[`UPSTREAM-COMMIT`](UPSTREAM-COMMIT)) plus these changes. The README's "What differs from
upstream" gives the detail of each.

| # | Change | Reason | Where |
|---|--------|--------|-------|
| 1 | Packet size: `MaxPacketBufferSize` 8192, `Config.MaxPacketSize`, `Conn.MaxUDPPayloadLimits` | Argus `e9d6c34`; Iris links have larger MTUs | `internal/protocol`, `config.go`, the MTU code and its tests |
| 2 | No output, no `QUIC_GO_*` variables | Data Path SDK requirement D106 | sites marked `nol8 (D106)` |
| 3 | Module path `github.com/nol8io/quic-go` | the fork's own module | every `go.mod` and import |
| 4 | Fork maintenance and security baseline | ARG-150 | `nol8/`, `.github/`, README |
| 5 | Client handshake completion callback | Hand close ownership to the SDK before dial returns, without packet tracing | `interface.go`, `config.go`, `connection.go` |

To see the exact diff, fetch the upstream commit in `UPSTREAM-COMMIT` and compare it with
`master` (see the README).
