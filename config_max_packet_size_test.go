package quic

import (
	"fmt"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

// MPS-1: populateConfig fills the patched 8192-byte default for MaxPacketSize,
// keeps a configured MaxPacketSize, and leaves InitialPacketSize as configured.
func TestMaxPacketSizeConfigDefaults(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		// MPS-1: populateConfig(nil) gives MaxPacketSize 8192.
		require.Equal(t, uint16(8192), populateConfig(nil).MaxPacketSize)
	})

	for _, tc := range []struct {
		name        string
		conf        *Config
		wantMax     uint16
		wantInitial uint16
	}{
		// MPS-1: an empty config gets the 8192 default; InitialPacketSize 0 stays 0.
		{name: "empty config", conf: &Config{}, wantMax: 8192, wantInitial: 0},
		// MPS-1: a configured MaxPacketSize is kept unchanged.
		{name: "MaxPacketSize 1200", conf: &Config{MaxPacketSize: 1200}, wantMax: 1200, wantInitial: 0},
		{name: "MaxPacketSize 1452", conf: &Config{MaxPacketSize: 1452}, wantMax: 1452, wantInitial: 0},
		{name: "MaxPacketSize 4096", conf: &Config{MaxPacketSize: 4096}, wantMax: 4096, wantInitial: 0},
		{name: "MaxPacketSize 8192", conf: &Config{MaxPacketSize: 8192}, wantMax: 8192, wantInitial: 0},
		// MPS-1: InitialPacketSize stays as configured.
		{name: "InitialPacketSize 1300", conf: &Config{InitialPacketSize: 1300}, wantMax: 8192, wantInitial: 1300},
		{
			name:        "InitialPacketSize 1300, MaxPacketSize 1452",
			conf:        &Config{InitialPacketSize: 1300, MaxPacketSize: 1452},
			wantMax:     1452,
			wantInitial: 1300,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			populated := populateConfig(tc.conf)
			require.Equal(t, tc.wantMax, populated.MaxPacketSize, "MaxPacketSize")
			require.Equal(t, tc.wantInitial, populated.InitialPacketSize, "InitialPacketSize")
		})
	}
}

// MPS-2: validateConfig accepts 0 and values in [1200, 8192] and rejects the
// rest with an error that names MaxPacketSize.
func TestMaxPacketSizeConfigValidation(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		// MPS-2: validateConfig(nil) returns nil.
		require.NoError(t, validateConfig(nil))
	})

	// MPS-2: accepted values.
	for _, size := range []uint16{0, 1200, 1452, 4096, 8192} {
		t.Run(fmt.Sprintf("valid MaxPacketSize %d", size), func(t *testing.T) {
			require.NoError(t, validateConfig(&Config{MaxPacketSize: size}))
		})
	}

	// MPS-2: rejected values, below 1200 or above protocol.MaxPacketBufferSize.
	for _, size := range []uint16{1, 1199, 8193, 65535} {
		t.Run(fmt.Sprintf("invalid MaxPacketSize %d", size), func(t *testing.T) {
			err := validateConfig(&Config{MaxPacketSize: size})
			require.Error(t, err)
			require.ErrorContains(t, err, "MaxPacketSize")
		})
	}
}

// MPS-3: validateConfig lowers an InitialPacketSize above a configured
// MaxPacketSize to that limit and leaves it unchanged otherwise.
func TestMaxPacketSizeConfigInitialPacketSizeLowered(t *testing.T) {
	for _, tc := range []struct {
		name        string
		conf        *Config
		wantInitial uint16
	}{
		// MPS-3: InitialPacketSize > MaxPacketSize becomes MaxPacketSize.
		{name: "initial 4000, limit 1452", conf: &Config{InitialPacketSize: 4000, MaxPacketSize: 1452}, wantInitial: 1452},
		// MPS-3: InitialPacketSize <= MaxPacketSize is unchanged.
		{name: "initial 1300, limit 1452", conf: &Config{InitialPacketSize: 1300, MaxPacketSize: 1452}, wantInitial: 1300},
		{name: "initial 1452, limit 1452", conf: &Config{InitialPacketSize: 1452, MaxPacketSize: 1452}, wantInitial: 1452},
		{name: "initial unset, limit 1452", conf: &Config{MaxPacketSize: 1452}, wantInitial: 0},
		// MPS-3: with MaxPacketSize 0 an InitialPacketSize of 4000 is unchanged.
		{name: "initial 4000, no limit", conf: &Config{InitialPacketSize: 4000}, wantInitial: 4000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, validateConfig(tc.conf))
			require.Equal(t, tc.wantInitial, tc.conf.InitialPacketSize)
		})
	}
}

// MPS-3: initialPacketSize is min(InitialPacketSize, or 1280 when it is 0, and
// the local limit from MPS-4), whether or not the Config was validated.
func TestMaxPacketSizeConfigInitialPacketSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		conf func() *Config
		want protocol.ByteCount
		// unvalidatedOnly marks a Config that validateConfig would change or
		// reject; populateConfig on it is out of scope, so only the raw Config
		// is checked.
		unvalidatedOnly bool
	}{
		// MPS-3: an explicit InitialPacketSize is returned as is.
		{name: "explicit 1300", conf: func() *Config { return &Config{InitialPacketSize: 1300} }, want: 1300},
		{
			name: "explicit 1300, limit 1452",
			conf: func() *Config { return &Config{InitialPacketSize: 1300, MaxPacketSize: 1452} },
			want: 1300,
		},
		{name: "explicit 4000, no limit", conf: func() *Config { return &Config{InitialPacketSize: 4000} }, want: 4000},
		// MPS-3: InitialPacketSize 0 gives 1280 for MaxPacketSize 0 or >= 1280.
		{name: "default, no limit", conf: func() *Config { return &Config{} }, want: 1280},
		{name: "default, limit 8192", conf: func() *Config { return &Config{MaxPacketSize: 8192} }, want: 1280},
		{name: "default, limit 1452", conf: func() *Config { return &Config{MaxPacketSize: 1452} }, want: 1280},
		{name: "default, limit 1280", conf: func() *Config { return &Config{MaxPacketSize: 1280} }, want: 1280},
		// MPS-3: InitialPacketSize 0 gives the limit when it is below 1280.
		{name: "default, limit 1250", conf: func() *Config { return &Config{MaxPacketSize: 1250} }, want: 1250},
		{name: "default, limit 1200", conf: func() *Config { return &Config{MaxPacketSize: 1200} }, want: 1200},
		// MPS-3: without validateConfig, the result is still capped by the
		// local limit from MPS-4.
		{
			name:            "unvalidated explicit 4000, limit 1452",
			conf:            func() *Config { return &Config{InitialPacketSize: 4000, MaxPacketSize: 1452} },
			want:            1452,
			unvalidatedOnly: true,
		},
		{
			name:            "unvalidated explicit 9000, no limit",
			conf:            func() *Config { return &Config{InitialPacketSize: 9000} },
			want:            8192,
			unvalidatedOnly: true,
		},
		// MPS-3 with MPS-4: unset with an unvalidated limit of 1000 gives
		// min(1280, 1200), because MPS-4 clamps 1000 to 1200.
		{
			name:            "unvalidated default, limit 1000",
			conf:            func() *Config { return &Config{MaxPacketSize: 1000} },
			want:            1200,
			unvalidatedOnly: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.conf().initialPacketSize(), "unpopulated config")
			if tc.unvalidatedOnly {
				return
			}
			// MPS-1 keeps both fields through populateConfig, so the result is the same.
			require.Equal(t, tc.want, populateConfig(tc.conf()).initialPacketSize(), "populated config")
		})
	}
}

// MPS-4: maxUDPPayloadSize returns 8192 for a nil config or MaxPacketSize 0,
// the configured MaxPacketSize for values in [1200, 8192], and an unvalidated
// value clamped into [1200, 8192].
func TestMaxPacketSizeConfigLocalLimit(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		// MPS-4: a nil *Config gives 8192.
		var conf *Config
		require.Equal(t, protocol.ByteCount(8192), conf.maxUDPPayloadSize())
	})

	t.Run("populated nil config", func(t *testing.T) {
		// MPS-1 and MPS-4: the populated default gives 8192.
		require.Equal(t, protocol.ByteCount(8192), populateConfig(nil).maxUDPPayloadSize())
	})

	for _, tc := range []struct {
		name string
		conf *Config
		want protocol.ByteCount
	}{
		// MPS-4: MaxPacketSize 0 gives 8192.
		{name: "MaxPacketSize 0", conf: &Config{}, want: 8192},
		// MPS-4: a configured MaxPacketSize is returned as is.
		{name: "MaxPacketSize 1200", conf: &Config{MaxPacketSize: 1200}, want: 1200},
		{name: "MaxPacketSize 1452", conf: &Config{MaxPacketSize: 1452}, want: 1452},
		{name: "MaxPacketSize 4096", conf: &Config{MaxPacketSize: 4096}, want: 4096},
		{name: "MaxPacketSize 8192", conf: &Config{MaxPacketSize: 8192}, want: 8192},
		// MPS-4: an unvalidated value below 1200 is clamped up to 1200.
		{name: "unvalidated MaxPacketSize 1", conf: &Config{MaxPacketSize: 1}, want: 1200},
		{name: "unvalidated MaxPacketSize 1000", conf: &Config{MaxPacketSize: 1000}, want: 1200},
		{name: "unvalidated MaxPacketSize 1199", conf: &Config{MaxPacketSize: 1199}, want: 1200},
		// MPS-4: an unvalidated value above 8192 is clamped down to 8192.
		{name: "unvalidated MaxPacketSize 8193", conf: &Config{MaxPacketSize: 8193}, want: 8192},
		{name: "unvalidated MaxPacketSize 9000", conf: &Config{MaxPacketSize: 9000}, want: 8192},
		{name: "unvalidated MaxPacketSize 65535", conf: &Config{MaxPacketSize: 65535}, want: 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.conf.maxUDPPayloadSize())
		})
	}
}

// MPS-11: newPathPacketSize is InitialPacketSize if set, otherwise 1200, and
// never above the local limit from MPS-4, whether or not the Config was
// validated.
func TestMaxPacketSizeConfigNewPathPacketSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		conf func() *Config
		want protocol.ByteCount
		// unvalidatedOnly marks a Config that validateConfig would change or
		// reject; the contract states its result only without validateConfig.
		unvalidatedOnly bool
	}{
		// MPS-11: InitialPacketSize unset gives 1200.
		{name: "empty config", conf: func() *Config { return &Config{} }, want: 1200},
		// MPS-11: an explicit InitialPacketSize is used.
		{name: "initial 1300", conf: func() *Config { return &Config{InitialPacketSize: 1300} }, want: 1300},
		// MPS-11: unset with a limit above 1200 still gives 1200.
		{name: "initial unset, limit 1452", conf: func() *Config { return &Config{MaxPacketSize: 1452} }, want: 1200},
		// MPS-11: an explicit InitialPacketSize below the limit is used.
		{
			name: "initial 1300, limit 1452",
			conf: func() *Config { return &Config{InitialPacketSize: 1300, MaxPacketSize: 1452} },
			want: 1300,
		},
		// MPS-11: without validateConfig, the result is capped by the MPS-4 limit.
		{
			name:            "unvalidated initial 4000, limit 1452",
			conf:            func() *Config { return &Config{InitialPacketSize: 4000, MaxPacketSize: 1452} },
			want:            1452,
			unvalidatedOnly: true,
		},
		{
			name:            "unvalidated initial 9000, no limit",
			conf:            func() *Config { return &Config{InitialPacketSize: 9000} },
			want:            8192,
			unvalidatedOnly: true,
		},
		// MPS-11 with MPS-4: the unvalidated limit 1000 clamps to 1200.
		{
			name:            "unvalidated initial 1300, limit 1000",
			conf:            func() *Config { return &Config{InitialPacketSize: 1300, MaxPacketSize: 1000} },
			want:            1200,
			unvalidatedOnly: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.conf().newPathPacketSize(), "unvalidated config")
			if tc.unvalidatedOnly {
				return
			}
			// MPS-11 "validated or not": validateConfig accepts these and leaves
			// both fields as they are (MPS-2, MPS-3), so the result is the same.
			conf := tc.conf()
			require.NoError(t, validateConfig(conf))
			require.Equal(t, tc.want, conf.newPathPacketSize(), "validated config")
		})
	}
}
