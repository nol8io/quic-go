package quic

import (
	"cmp"
	"fmt"
	"time"

	"github.com/nol8io/quic-go/internal/protocol"
	"github.com/nol8io/quic-go/quicvarint"
)

// Clone clones a Config.
func (c *Config) Clone() *Config {
	copy := *c
	return &copy
}

func (c *Config) handshakeTimeout() time.Duration {
	return 2 * c.HandshakeIdleTimeout
}

func (c *Config) maxRetryTokenAge() time.Duration {
	return c.handshakeTimeout()
}

func (c *Config) initialPacketSize() protocol.ByteCount {
	size := protocol.ByteCount(c.InitialPacketSize)
	if size == 0 {
		size = protocol.InitialPacketSize
	}
	// Never above MaxPacketSize, including for a Config from GetConfigForClient,
	// which doesn't go through validateConfig.
	return min(size, c.maxUDPPayloadSize())
}

func validateConfig(config *Config) error {
	if config == nil {
		return nil
	}
	const maxStreams = 1 << 60
	if config.MaxIncomingStreams > maxStreams {
		config.MaxIncomingStreams = maxStreams
	}
	if config.MaxIncomingUniStreams > maxStreams {
		config.MaxIncomingUniStreams = maxStreams
	}
	if config.MaxStreamReceiveWindow > quicvarint.Max {
		config.MaxStreamReceiveWindow = quicvarint.Max
	}
	if config.MaxConnectionReceiveWindow > quicvarint.Max {
		config.MaxConnectionReceiveWindow = quicvarint.Max
	}
	if config.InitialPacketSize > 0 && config.InitialPacketSize < protocol.MinInitialPacketSize {
		config.InitialPacketSize = protocol.MinInitialPacketSize
	}
	if config.InitialPacketSize > protocol.MaxPacketBufferSize {
		config.InitialPacketSize = protocol.MaxPacketBufferSize
	}
	if config.MaxPacketSize != 0 &&
		(config.MaxPacketSize < protocol.MinInitialPacketSize || protocol.ByteCount(config.MaxPacketSize) > protocol.MaxPacketBufferSize) {
		return fmt.Errorf("quic: MaxPacketSize %d out of range [%d, %d]",
			config.MaxPacketSize, protocol.MinInitialPacketSize, protocol.MaxPacketBufferSize)
	}
	if config.MaxPacketSize != 0 && config.InitialPacketSize > config.MaxPacketSize {
		config.InitialPacketSize = config.MaxPacketSize
	}
	// check that all QUIC versions are actually supported
	for _, v := range config.Versions {
		if !protocol.IsValidVersion(v) {
			return fmt.Errorf("invalid QUIC version: %s", v)
		}
	}
	return nil
}

// maxUDPPayloadSize is the per-connection UDP payload ceiling advertised in
// max_udp_payload_size and used as the Path MTU discovery upper bound.
// The packet buffer pool stays sized to protocol.MaxPacketBufferSize.
// validateConfig rejects a MaxPacketSize outside [MinInitialPacketSize,
// MaxPacketBufferSize], but a Config from GetConfigForClient isn't validated, so
// the range is enforced here too: probing above the buffer size would panic.
func (c *Config) maxUDPPayloadSize() protocol.ByteCount {
	if c == nil || c.MaxPacketSize == 0 {
		return protocol.MaxPacketBufferSize
	}
	return min(max(protocol.ByteCount(c.MaxPacketSize), protocol.MinInitialPacketSize), protocol.MaxPacketBufferSize)
}

// newPathPacketSize is the packet size a connection restarts at on a new path:
// InitialPacketSize if set, otherwise MinInitialPacketSize, never above
// MaxPacketSize (a Config from GetConfigForClient isn't validated).
func (c *Config) newPathPacketSize() protocol.ByteCount {
	return min(protocol.ByteCount(cmp.Or(c.InitialPacketSize, protocol.MinInitialPacketSize)), c.maxUDPPayloadSize())
}

// populateConfig populates fields in the quic.Config with their default values, if none are set
// it may be called with nil
// InitialPacketSize keeps zero to distinguish the default from an explicit packet-size floor.
func populateConfig(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}
	versions := config.Versions
	if len(versions) == 0 {
		versions = protocol.SupportedVersions
	}
	handshakeIdleTimeout := protocol.DefaultHandshakeIdleTimeout
	if config.HandshakeIdleTimeout != 0 {
		handshakeIdleTimeout = config.HandshakeIdleTimeout
	}
	idleTimeout := protocol.DefaultIdleTimeout
	if config.MaxIdleTimeout != 0 {
		idleTimeout = config.MaxIdleTimeout
	}
	initialStreamReceiveWindow := config.InitialStreamReceiveWindow
	if initialStreamReceiveWindow == 0 {
		initialStreamReceiveWindow = protocol.DefaultInitialMaxStreamData
	}
	maxStreamReceiveWindow := config.MaxStreamReceiveWindow
	if maxStreamReceiveWindow == 0 {
		maxStreamReceiveWindow = protocol.DefaultMaxReceiveStreamFlowControlWindow
	}
	initialConnectionReceiveWindow := config.InitialConnectionReceiveWindow
	if initialConnectionReceiveWindow == 0 {
		initialConnectionReceiveWindow = protocol.DefaultInitialMaxData
	}
	maxConnectionReceiveWindow := config.MaxConnectionReceiveWindow
	if maxConnectionReceiveWindow == 0 {
		maxConnectionReceiveWindow = protocol.DefaultMaxReceiveConnectionFlowControlWindow
	}
	maxIncomingStreams := config.MaxIncomingStreams
	if maxIncomingStreams == 0 {
		maxIncomingStreams = protocol.DefaultMaxIncomingStreams
	} else if maxIncomingStreams < 0 {
		maxIncomingStreams = 0
	}
	maxIncomingUniStreams := config.MaxIncomingUniStreams
	if maxIncomingUniStreams == 0 {
		maxIncomingUniStreams = protocol.DefaultMaxIncomingUniStreams
	} else if maxIncomingUniStreams < 0 {
		maxIncomingUniStreams = 0
	}
	maxPacketSize := config.MaxPacketSize
	if maxPacketSize == 0 {
		maxPacketSize = uint16(protocol.MaxPacketBufferSize)
	}

	return &Config{
		GetConfigForClient:               config.GetConfigForClient,
		Versions:                         versions,
		HandshakeIdleTimeout:             handshakeIdleTimeout,
		MaxIdleTimeout:                   idleTimeout,
		KeepAlivePeriod:                  config.KeepAlivePeriod,
		InitialStreamReceiveWindow:       initialStreamReceiveWindow,
		MaxStreamReceiveWindow:           maxStreamReceiveWindow,
		InitialConnectionReceiveWindow:   initialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:       maxConnectionReceiveWindow,
		AllowConnectionWindowIncrease:    config.AllowConnectionWindowIncrease,
		MaxIncomingStreams:               maxIncomingStreams,
		MaxIncomingUniStreams:            maxIncomingUniStreams,
		RequireStreamCreditRelease:       config.RequireStreamCreditRelease,
		TokenStore:                       config.TokenStore,
		EnableDatagrams:                  config.EnableDatagrams,
		InitialPacketSize:                config.InitialPacketSize,
		MaxPacketSize:                    maxPacketSize,
		DisablePathMTUDiscovery:          config.DisablePathMTUDiscovery,
		EnableStreamResetPartialDelivery: config.EnableStreamResetPartialDelivery,
		Allow0RTT:                        config.Allow0RTT,
		Tracer:                           config.Tracer,
	}
}
