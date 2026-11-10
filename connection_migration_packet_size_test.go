package quic

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// The peer's transport parameters in every MPS-12 case. The peer accepts
// datagrams as large as the largest packet buffer, so it never lowers the
// packet size below the local limit.
const (
	mps12PeerMaxUDPPayloadSize    = 8192
	mps12PeerMaxDatagramFrameSize = 8192
)

// TestConnectionMigrationNewPathPacketSize covers MPS-12: when a connection
// moves to a new network path, its Path MTU discoverer restarts at the MPS-11
// new-path packet size (InitialPacketSize if set, otherwise 1200, never above
// the local limit: MaxPacketSize clamped into [1200, 8192], 8192 when 0).
//
// The test-connection helpers populate the Config but never validate it, as
// for a Config returned by GetConfigForClient, so InitialPacketSize can exceed
// the local limit. Upstream restarts at the raw InitialPacketSize; the rows
// with InitialPacketSize 1500 and 9000 fail under that rule.
func TestConnectionMigrationNewPathPacketSize(t *testing.T) {
	cases := []struct {
		name      string
		newConfig func() *Config
		expected  protocol.ByteCount
	}{
		{
			// MPS-12, row {}: no InitialPacketSize, so the new path starts at 1200.
			name:      "default config",
			newConfig: func() *Config { return &Config{} },
			expected:  1200,
		},
		{
			// MPS-12, row {InitialPacketSize: 1234}: within the local limit, used as is.
			name:      "InitialPacketSize 1234",
			newConfig: func() *Config { return &Config{InitialPacketSize: 1234} },
			expected:  1234,
		},
		{
			// MPS-12, row {InitialPacketSize: 1500, MaxPacketSize: 1300}: capped at
			// MaxPacketSize. Upstream's rule would restart at 1500.
			name:      "InitialPacketSize 1500 above MaxPacketSize 1300",
			newConfig: func() *Config { return &Config{InitialPacketSize: 1500, MaxPacketSize: 1300} },
			expected:  1300,
		},
		{
			// MPS-12, row {InitialPacketSize: 9000}: MaxPacketSize 0 means 8192, so
			// capped at 8192. Upstream's rule would restart at 9000.
			name:      "InitialPacketSize 9000 above default limit",
			newConfig: func() *Config { return &Config{InitialPacketSize: 9000} },
			expected:  8192,
		},
		{
			// MPS-12, row {InitialPacketSize: 1300, MaxPacketSize: 1000}: MaxPacketSize
			// is raised to 1200, which caps InitialPacketSize.
			name:      "MaxPacketSize 1000 below minimum",
			newConfig: func() *Config { return &Config{InitialPacketSize: 1300, MaxPacketSize: 1000} },
			expected:  1200,
		},
	}

	// Neither variant schedules an MTU probe on the new path, so the templates'
	// mock expectations hold. MPS-12 holds whether or not discovery is enabled.
	noProbeVariants := []struct {
		name       string
		disableMTU bool
		df         bool
	}{
		{name: "MTU discovery disabled", disableMTU: true, df: true},
		{name: "DF unsupported", disableMTU: false, df: false},
	}

	moves := []struct {
		name string
		run  func(t *testing.T, config *Config, df bool, expected protocol.ByteCount)
	}{
		// MPS-12 (a): the server validates a NAT rebinding and switches to the new path.
		{name: "server NAT rebinding", run: testNewPathPacketSizeServerNATRebinding},
		// MPS-12 (b): the client switches to a new path.
		{name: "client switchToNewPath", run: testNewPathPacketSizeClientSwitch},
	}

	for _, move := range moves {
		t.Run(move.name, func(t *testing.T) {
			for _, row := range cases {
				t.Run(row.name, func(t *testing.T) {
					for _, variant := range noProbeVariants {
						t.Run(variant.name, func(t *testing.T) {
							config := row.newConfig()
							config.DisablePathMTUDiscovery = variant.disableMTU
							config.EnableDatagrams = true
							move.run(t, config, variant.df, row.expected)
						})
					}
				})
			}
		})
	}
}

// testNewPathPacketSizeServerNATRebinding is MPS-12 (a), following the
// testConnectionPathValidation template with isNATRebinding = true: the server
// receives a non-probing packet from a new remote address, sends a
// PATH_CHALLENGE there, receives the PATH_RESPONSE from that address and
// switches to it.
func testNewPathPacketSizeServerNATRebinding(t *testing.T, config *Config, df bool, expected protocol.ByteCount) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t, gomock.WithOverridableExpectations())
		unpacker := NewMockUnpacker(mockCtrl)
		tc := newServerTestConnection(
			t,
			mockCtrl,
			config,
			false,
			connectionOptUnpacker(unpacker),
			connectionOptHandshakeConfirmed(),
			connectionOptRTT(time.Second),
		)
		tc.sendConn.EXPECT().capabilities().Return(connCapabilities{DF: df}).AnyTimes()
		require.NoError(t, tc.conn.handleTransportParameters(&wire.TransportParameters{
			MaxUDPPayloadSize:         mps12PeerMaxUDPPayloadSize,
			MaxDatagramFrameSize:      mps12PeerMaxDatagramFrameSize,
			InitialSourceConnectionID: protocol.ZeroLengthConnectionID,
		}))
		// Raise the packet size on the current path where there is room, so that
		// a move that keeps the old size cannot pass for a restart.
		ping, _ := tc.conn.mtuDiscoverer.GetPing(monotime.Now())
		ping.Handler.OnAcked(ping.Frame)
		tc.conn.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(tc.conn.mtuDiscoverer.CurrentSize())))

		newRemoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 1234}
		require.NotEqual(t, tc.remoteAddr, newRemoteAddr)

		errChan := make(chan error, 1)
		go func() { errChan <- tc.conn.run() }()

		// A non-probing packet (a PING frame) arrives from the new remote address.
		// The server sends a PATH_CHALLENGE to that address.
		probeSent := make(chan struct{})
		var pathChallenge *wire.PathChallengeFrame
		gomock.InOrder(
			unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(
				protocol.PacketNumber(10), protocol.PacketNumberLen2, protocol.KeyPhaseZero, []byte{1}, nil,
			),
			tc.packer.EXPECT().PackPathProbePacket(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ protocol.ConnectionID, frames []ackhandler.Frame, _ protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
					pathChallenge = frames[0].Frame.(*wire.PathChallengeFrame)
					return shortHeaderPacket{IsPathProbePacket: true}, getPacketBuffer(), nil
				},
			),
			tc.sendConn.EXPECT().WriteTo(gomock.Any(), newRemoteAddr, packetInfo{}).DoAndReturn(
				func([]byte, net.Addr, packetInfo) error { close(probeSent); return nil },
			),
			tc.packer.EXPECT().AppendPacket(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(
				shortHeaderPacket{}, errNothingToPack,
			),
		)

		tc.conn.handlePacket(receivedPacket{
			data:       make([]byte, 10),
			buffer:     getPacketBuffer(),
			remoteAddr: newRemoteAddr,
			rcvTime:    monotime.Now(),
		})

		synctest.Wait()

		select {
		case <-probeSent:
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}

		// The PATH_RESPONSE arrives from the new address. Since the first packet on
		// that path was non-probing (a NAT rebinding), the server switches to it.
		migrated := make(chan struct{})
		data, err := (&wire.PathResponseFrame{Data: pathChallenge.Data}).Append(nil, protocol.Version1)
		require.NoError(t, err)
		gomock.InOrder(
			unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(
				protocol.PacketNumber(11), protocol.PacketNumberLen2, protocol.KeyPhaseZero, data, nil,
			),
			tc.sendConn.EXPECT().ChangeRemoteAddr(newRemoteAddr, gomock.Any()).Do(
				func(net.Addr, packetInfo) { close(migrated) },
			),
			tc.packer.EXPECT().AppendPacket(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(
				shortHeaderPacket{}, errNothingToPack,
			).MaxTimes(1),
		)
		require.Equal(t, tc.remoteAddr, tc.conn.RemoteAddr())
		tc.conn.handlePacket(receivedPacket{
			data:       make([]byte, 100),
			buffer:     getPacketBuffer(),
			remoteAddr: newRemoteAddr,
			rcvTime:    monotime.Now(),
		})

		synctest.Wait()

		select {
		case <-migrated:
		default:
			t.Fatal("should have migrated")
		}
		sizeAfterMove := tc.conn.mtuDiscoverer.CurrentSize()

		// test teardown
		tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
		tc.conn.destroy(nil)

		synctest.Wait()

		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
			t.Fatal("should have shut down")
		}

		// MPS-12: asserted after teardown, so a wrong size fails the case without
		// leaving the run loop behind in the bubble.
		require.Equal(t, expected, sizeAfterMove, "Path MTU discoverer after the move must restart at the MPS-11 new-path packet size")
	})
}

// testNewPathPacketSizeClientSwitch is MPS-12 (b), following the
// TestConnectionMigrationMTUDiscovery template: the client moves to a new path
// with switchToNewPath.
func testNewPathPacketSizeClientSwitch(t *testing.T, config *Config, df bool, expected protocol.ByteCount) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		oldSender := NewMockSender(mockCtrl)
		tc := newClientTestConnection(t, mockCtrl,
			config,
			false, connectionOptSender(oldSender), connectionOptHandshakeConfirmed(),
		)
		require.NoError(t, tc.conn.handleTransportParameters(&wire.TransportParameters{
			InitialSourceConnectionID:       tc.destConnID,
			OriginalDestinationConnectionID: tc.destConnID,
			MaxUDPPayloadSize:               mps12PeerMaxUDPPayloadSize,
			MaxDatagramFrameSize:            mps12PeerMaxDatagramFrameSize,
		}))
		tc.conn.applyTransportParameters()
		now := monotime.Now()
		// Raise the packet size on the current path where there is room, so that
		// a move that keeps the old size cannot pass for a restart.
		ping, _ := tc.conn.mtuDiscoverer.GetPing(now)
		ping.Handler.OnAcked(ping.Frame)

		conn := NewMockRawConn(mockCtrl)
		conn.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4321})
		conn.EXPECT().capabilities().Return(connCapabilities{DF: df}).AnyTimes()
		oldSender.EXPECT().Close()
		tc.conn.switchToNewPath(&Transport{conn: conn}, now)
		defer tc.conn.sendQueue.Close()

		// The move happened: the old send queue was closed (expected above) and
		// replaced by one for the new path.
		require.NotEqual(t, oldSender, tc.conn.sendQueue)

		// MPS-12
		require.Equal(t, expected, tc.conn.mtuDiscoverer.CurrentSize(), "Path MTU discoverer after the move must restart at the MPS-11 new-path packet size")
	})
}
