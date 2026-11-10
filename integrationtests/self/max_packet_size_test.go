package self_test

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// maxPacketSizeConnLimits returns conn.MaxUDPPayloadLimits() as (local, peer, effective).
func maxPacketSizeConnLimits(conn *quic.Conn) [3]int64 {
	local, peer, effective := conn.MaxUDPPayloadLimits()
	return [3]int64{local, peer, effective}
}

// MPS-7: each side of a real handshake reports its own configured limit as
// local and the other side's configured limit as peer, whichever side
// configured it.
func TestMaxPacketSizeAdvertised(t *testing.T) {
	for _, tc := range []struct {
		name                string
		clientMaxPacketSize uint16   // 0 is the default config
		serverMaxPacketSize uint16   // 0 is the default config
		clientLimits        [3]int64 // client (local, peer, effective)
		serverLimits        [3]int64 // server (local, peer, effective)
	}{
		// MPS-7 (a): server 4096, client default: the client sees the server's
		// 4096 and the server sees the client's default 8192.
		{
			name:                "server MaxPacketSize 4096, client default",
			serverMaxPacketSize: 4096,
			clientLimits:        [3]int64{8192, 4096, 4096},
			serverLimits:        [3]int64{4096, 8192, 4096},
		},
		// MPS-7 (b): client 4096, server default: the server sees the client's
		// 4096 and the client sees the server's default 8192.
		{
			name:                "client MaxPacketSize 4096, server default",
			clientMaxPacketSize: 4096,
			clientLimits:        [3]int64{4096, 8192, 4096},
			serverLimits:        [3]int64{8192, 4096, 4096},
		},
		// MPS-7 (c): client 1452, server 4096: both sides see the other's
		// configured limit, and both effective limits are 1452.
		{
			name:                "client MaxPacketSize 1452, server MaxPacketSize 4096",
			clientMaxPacketSize: 1452,
			serverMaxPacketSize: 4096,
			clientLimits:        [3]int64{1452, 4096, 1452},
			serverLimits:        [3]int64{4096, 1452, 1452},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := quic.Listen(
				newUDPConnLocalhost(t),
				getTLSConfig(),
				getQuicConfig(&quic.Config{MaxPacketSize: tc.serverMaxPacketSize}),
			)
			require.NoError(t, err)
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(5*time.Second))
			defer cancel()
			clientConn, err := quic.Dial(
				ctx,
				newUDPConnLocalhost(t),
				server.Addr(),
				getTLSClientConfig(),
				getQuicConfig(&quic.Config{MaxPacketSize: tc.clientMaxPacketSize}),
			)
			require.NoError(t, err)
			defer clientConn.CloseWithError(0, "")

			serverConn, err := server.Accept(ctx)
			require.NoError(t, err)
			defer serverConn.CloseWithError(0, "")

			// MPS-7: limits after quic.Dial and server.Accept have returned.
			require.Equal(t, tc.clientLimits, maxPacketSizeConnLimits(clientConn), "client (local, peer, effective)")
			require.Equal(t, tc.serverLimits, maxPacketSizeConnLimits(serverConn), "server (local, peer, effective)")
		})
	}
}

// MPS-8: PMTUD on loopback grows the client's packets past upstream's 1452
// cap and never above the lower of the two configured limits.
func TestMaxPacketSizePMTUD(t *testing.T) {
	for _, tc := range []struct {
		name                string
		clientMaxPacketSize uint16 // 0 is the default config
		serverMaxPacketSize uint16 // 0 is the default config
		mtuAbove            int    // the discovered MTU must be greater than this; 0 means no lower bound
		mtuAtMost           int
		clientLimits        [3]int64 // client (local, peer, effective), per MPS-6 and MPS-7
	}{
		// MPS-8 (a): default configs: 1452 < MTU <= 8192.
		{
			name:         "default configs",
			mtuAbove:     1452,
			mtuAtMost:    8192,
			clientLimits: [3]int64{8192, 8192, 8192},
		},
		// MPS-8 (b): client MaxPacketSize 4096, server default: 1452 < MTU <= 4096.
		{
			name:                "client MaxPacketSize 4096",
			clientMaxPacketSize: 4096,
			mtuAbove:            1452,
			mtuAtMost:           4096,
			clientLimits:        [3]int64{4096, 8192, 4096},
		},
		// MPS-8 (c): client default, server MaxPacketSize 1452: the peer's
		// advertisement lowers the client's ceiling, so MTU <= 1452.
		{
			name:                "server MaxPacketSize 1452",
			serverMaxPacketSize: 1452,
			mtuAtMost:           1452,
			clientLimits:        [3]int64{8192, 1452, 1452},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mtu, limits := maxPacketSizeDiscoverMTU(t, tc.clientMaxPacketSize, tc.serverMaxPacketSize)
			t.Logf("discovered MTU %d, client limits %v", mtu, limits)

			require.Equal(t, tc.clientLimits, limits, "client (local, peer, effective)")
			if tc.mtuAbove > 0 {
				require.Greater(t, mtu, tc.mtuAbove, "PMTUD should grow past upstream's 1452 cap")
			}
			require.LessOrEqual(t, mtu, tc.mtuAtMost, "PMTUD must not exceed the effective limit")
		})
	}
}

// maxPacketSizeDiscoverMTU sends stream data from a client to a server on loopback
// until the client's qlog reports an MTUUpdated event with Done == true. It
// returns that event's Value and the client's MaxUDPPayloadLimits after the
// server has received the whole stream. It fails the test if PMTUD does not
// finish before the deadline (MPS-8).
func maxPacketSizeDiscoverMTU(t *testing.T, clientMaxPacketSize, serverMaxPacketSize uint16) (int, [3]int64) {
	t.Helper()

	server, err := quic.Listen(
		newUDPConnLocalhost(t),
		getTLSConfig(),
		getQuicConfig(&quic.Config{MaxPacketSize: serverMaxPacketSize}),
	)
	require.NoError(t, err)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(10*time.Second))
	defer cancel()
	var eventRecorder events.Recorder
	clientConn, err := quic.Dial(
		ctx,
		newUDPConnLocalhost(t),
		server.Addr(),
		getTLSClientConfig(),
		getQuicConfig(&quic.Config{
			MaxPacketSize: clientMaxPacketSize,
			Tracer:        newTracer(&eventRecorder),
		}),
	)
	require.NoError(t, err)
	defer clientConn.CloseWithError(0, "")

	serverConn, err := server.Accept(ctx)
	require.NoError(t, err)

	serverErrChan := make(chan error, 1)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		str, err := serverConn.AcceptStream(ctx)
		if err != nil {
			serverErrChan <- err
			return
		}
		_, err = io.Copy(io.Discard, str)
		serverErrChan <- err
	}()
	// Closing the server connection unblocks the goroutine if the test fails
	// early; wait for it so no goroutine outlives the test.
	defer func() {
		serverConn.CloseWithError(0, "")
		select {
		case <-serverDone:
		case <-time.After(scaleDuration(5 * time.Second)):
			t.Error("server stream goroutine did not exit after the connection closed")
		}
	}()

	str, err := clientConn.OpenStream()
	require.NoError(t, err)

	data := bytes.Repeat([]byte("d"), 16*1024)
	var discoveredMTU int
	for discoveredMTU == 0 {
		_, err := str.Write(data)
		require.NoError(t, err)
		if mtuEvents := eventRecorder.Events(qlog.MTUUpdated{}); len(mtuEvents) > 0 {
			update, ok := mtuEvents[len(mtuEvents)-1].(qlog.MTUUpdated)
			require.True(t, ok, "unexpected event type %T", mtuEvents[len(mtuEvents)-1])
			if update.Done {
				discoveredMTU = update.Value
			}
		}
		require.NoError(t, ctx.Err(), "PMTUD did not report an MTUUpdated event with Done before the deadline")
	}
	require.NoError(t, str.Close())

	// Receiving the stream FIN guarantees that the client applied the MTU update observed above.
	select {
	case err := <-serverErrChan:
		require.NoError(t, err)
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}

	return discoveredMTU, maxPacketSizeConnLimits(clientConn)
}

// MPS-9: MaxUDPPayloadLimits is safe to call from another goroutine while a
// 0-RTT handshake is still running, and only ever reports the values before or
// after the server's transport parameters apply. A data race fails the test
// under -race.
func TestMaxPacketSizeLimitsDuring0RTT(t *testing.T) {
	ln, err := quic.ListenEarly(
		newUDPConnLocalhost(t),
		getTLSConfig(),
		getQuicConfig(&quic.Config{Allow0RTT: true, MaxPacketSize: 4096}),
	)
	require.NoError(t, err)
	defer ln.Close()

	clientTLSConf := dialAndReceiveTicket(t, ln, newUDPConnLocalhost(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(5*time.Second))
	defer cancel()
	clientConn, err := quic.DialEarly(ctx, newUDPConnLocalhost(t), ln.Addr(), clientTLSConf, getQuicConfig(nil))
	require.NoError(t, err)
	defer clientConn.CloseWithError(0, "")

	// MPS-9: the only client values allowed while the handshake runs.
	beforeApplied := [3]int64{8192, 0, 8192}
	afterApplied := [3]int64{8192, 4096, 4096}

	// The poller writes these; the test reads them only after pollerDone is closed.
	var (
		calls           int
		unexpected      int
		firstUnexpected [3]int64
	)
	stopPoller := make(chan struct{})
	pollerDone := make(chan struct{})
	// MPS-9: started right after DialEarly returns; polls until HandshakeComplete is closed.
	go func() {
		defer close(pollerDone)
		for {
			if got := maxPacketSizeConnLimits(clientConn); got != beforeApplied && got != afterApplied {
				if unexpected == 0 {
					firstUnexpected = got
				}
				unexpected++
			}
			calls++
			select {
			case <-clientConn.HandshakeComplete():
				return
			case <-stopPoller: // only if the test fails before the handshake completes
				return
			default:
			}
		}
	}()
	defer func() {
		close(stopPoller)
		<-pollerDone
	}()

	serverConn, err := ln.Accept(ctx)
	require.NoError(t, err)
	defer serverConn.CloseWithError(0, "")

	select {
	case <-clientConn.HandshakeComplete():
	case <-ctx.Done():
		t.Fatal("handshake did not complete before the deadline")
	}
	select {
	case <-pollerDone:
	case <-time.After(scaleDuration(time.Second)):
		t.Fatal("poller did not exit after the handshake completed")
	}

	// MPS-9: the connection used 0-RTT, so the poller ran during a 0-RTT handshake.
	require.True(t, clientConn.ConnectionState().Used0RTT, "Used0RTT")
	require.Positive(t, calls, "poller calls")
	require.Zero(t, unexpected, "unexpected (local, peer, effective) results; first: %v", firstUnexpected)
	// MPS-9: after HandshakeComplete and after the poller exited.
	require.Equal(t, afterApplied, maxPacketSizeConnLimits(clientConn), "client (local, peer, effective)")
}

// MPS-10: a Config returned by GetConfigForClient skips validateConfig; the
// handshake still completes and the clamped limit applies on both sides.
func TestMaxPacketSizeGetConfigForClient(t *testing.T) {
	for _, tc := range []struct {
		name          string
		maxPacketSize uint16   // MaxPacketSize of the unvalidated Config from GetConfigForClient
		clientLimits  [3]int64 // client (local, peer, effective)
		serverLimits  [3]int64 // server (local, peer, effective)
	}{
		// MPS-10 (a): 9000 is clamped to 8192 on the server.
		{
			name:          "MaxPacketSize 9000",
			maxPacketSize: 9000,
			clientLimits:  [3]int64{8192, 8192, 8192},
			serverLimits:  [3]int64{8192, 8192, 8192},
		},
		// MPS-10 (b): 1000 is clamped to 1200 on the server.
		{
			name:          "MaxPacketSize 1000",
			maxPacketSize: 1000,
			clientLimits:  [3]int64{8192, 1200, 1200},
			serverLimits:  [3]int64{1200, 8192, 1200},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var getConfigCalls atomic.Int32
			server, err := quic.Listen(
				newUDPConnLocalhost(t),
				getTLSConfig(),
				getQuicConfig(&quic.Config{
					GetConfigForClient: func(*quic.ClientInfo) (*quic.Config, error) {
						getConfigCalls.Add(1)
						return &quic.Config{MaxPacketSize: tc.maxPacketSize}, nil
					},
				}),
			)
			require.NoError(t, err)
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(5*time.Second))
			defer cancel()
			// MPS-10: a default client completes the handshake.
			clientConn, err := quic.Dial(
				ctx,
				newUDPConnLocalhost(t),
				server.Addr(),
				getTLSClientConfig(),
				getQuicConfig(&quic.Config{}),
			)
			require.NoError(t, err)
			defer clientConn.CloseWithError(0, "")

			serverConn, err := server.Accept(ctx)
			require.NoError(t, err)
			defer serverConn.CloseWithError(0, "")

			// MPS-10 setup: the server used the Config from GetConfigForClient.
			require.Positive(t, getConfigCalls.Load(), "GetConfigForClient calls")
			require.Equal(t, tc.clientLimits, maxPacketSizeConnLimits(clientConn), "client (local, peer, effective)")
			require.Equal(t, tc.serverLimits, maxPacketSizeConnLimits(serverConn), "server (local, peer, effective)")
		})
	}
}
