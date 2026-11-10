package quic

import (
	"testing"

	"github.com/nol8io/quic-go/internal/protocol"
	"github.com/nol8io/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

// maxPacketSizeConnCase is one MPS-5/MPS-6 case: a connection config limit and
// the peer's transport parameters (nil while they are unknown).
type maxPacketSizeConnCase struct {
	name          string
	maxPacketSize uint16 // Config.MaxPacketSize; 0 is the default config
	peerParams    func() *wire.TransportParameters

	wantMaxMTU    protocol.ByteCount
	wantLocal     int64
	wantPeer      int64
	wantEffective int64
}

func maxPacketSizeConnCases() []maxPacketSizeConnCase {
	peer := func(size protocol.ByteCount) func() *wire.TransportParameters {
		return func() *wire.TransportParameters {
			return &wire.TransportParameters{MaxUDPPayloadSize: size}
		}
	}
	unknown := func() *wire.TransportParameters { return nil }

	return []maxPacketSizeConnCase{
		// MPS-5/MPS-6: MaxPacketSize 4096, peer unknown -> 4096; (4096, 0, 4096).
		{
			name: "MaxPacketSize 4096, peer unknown", maxPacketSize: 4096, peerParams: unknown,
			wantMaxMTU: 4096, wantLocal: 4096, wantPeer: 0, wantEffective: 4096,
		},
		// MPS-5/MPS-6: a peer MaxUDPPayloadSize of 0 counts as unknown.
		{
			name: "MaxPacketSize 4096, peer 0", maxPacketSize: 4096, peerParams: peer(0),
			wantMaxMTU: 4096, wantLocal: 4096, wantPeer: 0, wantEffective: 4096,
		},
		// MPS-5/MPS-6: MaxPacketSize 4096, peer 8192 -> 4096; (4096, 8192, 4096).
		{
			name: "MaxPacketSize 4096, peer 8192", maxPacketSize: 4096, peerParams: peer(8192),
			wantMaxMTU: 4096, wantLocal: 4096, wantPeer: 8192, wantEffective: 4096,
		},
		// MPS-5/MPS-6: MaxPacketSize 4096, peer 1452 -> 1452; (4096, 1452, 1452).
		{
			name: "MaxPacketSize 4096, peer 1452", maxPacketSize: 4096, peerParams: peer(1452),
			wantMaxMTU: 1452, wantLocal: 4096, wantPeer: 1452, wantEffective: 1452,
		},
		// MPS-5/MPS-6: default config, peer unknown -> 8192; (8192, 0, 8192).
		{
			name: "default config, peer unknown", maxPacketSize: 0, peerParams: unknown,
			wantMaxMTU: 8192, wantLocal: 8192, wantPeer: 0, wantEffective: 8192,
		},
		// MPS-5/MPS-6: default config, peer 65527 -> 8192; (8192, 65527, 8192).
		{
			name: "default config, peer 65527", maxPacketSize: 0, peerParams: peer(65527),
			wantMaxMTU: 8192, wantLocal: 8192, wantPeer: 65527, wantEffective: 8192,
		},
	}
}

// MPS-5: maxMTU is the local limit while the peer value is unknown, and
// min(local limit, peer MaxUDPPayloadSize) once it is known.
func TestMaxPacketSizeConnMaxMTU(t *testing.T) {
	for _, c := range maxPacketSizeConnCases() {
		t.Run(c.name, func(t *testing.T) {
			tc := newClientTestConnection(t, nil, &Config{MaxPacketSize: c.maxPacketSize}, false)
			tc.conn.peerParams = c.peerParams()

			// MPS-5: the local limit is the connection config's maxUDPPayloadSize().
			require.Equal(t, protocol.ByteCount(c.wantLocal), tc.conn.config.maxUDPPayloadSize(), "local limit")
			require.Equal(t, c.wantMaxMTU, tc.conn.maxMTU(), "maxMTU")
		})
	}
}

// MPS-6: MaxUDPPayloadLimits reports (local, 0, local) until the peer's
// transport parameters have been applied, and (local, peer, min(local, peer))
// after; a peer value of 0 is reported as (local, 0, local).
func TestMaxPacketSizeConnLimits(t *testing.T) {
	for _, c := range maxPacketSizeConnCases() {
		t.Run(c.name, func(t *testing.T) {
			tc := newClientTestConnection(t, nil, &Config{MaxPacketSize: c.maxPacketSize}, false)
			limits := func() [3]int64 {
				local, peer, effective := tc.conn.MaxUDPPayloadLimits()
				return [3]int64{local, peer, effective}
			}

			// MPS-6: setting peerParams without applying them leaves the result unchanged.
			tc.conn.peerParams = c.peerParams()
			require.Equal(t, [3]int64{c.wantLocal, 0, c.wantLocal}, limits(), "peerParams set, not applied")

			// MPS-6: once applied, the peer value is reported. In the "peer
			// unknown" cases there is nothing to apply and the want values are
			// (local, 0, local).
			if tc.conn.peerParams != nil {
				tc.conn.applyTransportParameters()
			}
			require.Equal(t, [3]int64{c.wantLocal, c.wantPeer, c.wantEffective}, limits(), "after apply")
		})
	}
}
