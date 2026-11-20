package utils

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLogLevelNothing(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	DefaultLogger.SetLogLevel(LogLevelNothing)
	DefaultLogger.Debugf("debug")
	DefaultLogger.Infof("info")
	DefaultLogger.Errorf("err")
	require.Empty(t, b.String())
}

func TestLogLevelError(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	DefaultLogger.SetLogLevel(LogLevelError)
	DefaultLogger.Debugf("debug")
	DefaultLogger.Infof("info")
	DefaultLogger.Errorf("err")
	require.Contains(t, b.String(), "err\n")
	require.NotContains(t, b.String(), "info")
	require.NotContains(t, b.String(), "debug")
}

func TestLogLevelInfo(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	DefaultLogger.SetLogLevel(LogLevelInfo)
	DefaultLogger.Debugf("debug")
	DefaultLogger.Infof("info")
	DefaultLogger.Errorf("err")
	require.Contains(t, b.String(), "err\n")
	require.Contains(t, b.String(), "info\n")
	require.NotContains(t, b.String(), "debug")
}

func TestLogLevelDebug(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	require.False(t, DefaultLogger.Debug())
	DefaultLogger.SetLogLevel(LogLevelDebug)
	require.True(t, DefaultLogger.Debug())
	DefaultLogger.Debugf("debug")
	DefaultLogger.Infof("info")
	DefaultLogger.Errorf("err")
	require.Contains(t, b.String(), "err\n")
	require.Contains(t, b.String(), "info\n")
	require.Contains(t, b.String(), "debug\n")
}

func TestNoTimestampWithEmptyFormat(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	DefaultLogger.SetLogLevel(LogLevelDebug)
	DefaultLogger.SetLogTimeFormat("")
	DefaultLogger.Debugf("debug")
	require.Equal(t, "debug\n", b.String())
}

func TestAddTimestamp(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	format := "Jan 2, 2006"
	DefaultLogger.SetLogTimeFormat(format)
	DefaultLogger.SetLogLevel(LogLevelInfo)
	DefaultLogger.Infof("info")
	timestamp := b.String()[:b.Len()-6]
	parsedTime, err := time.ParseInLocation(format, timestamp, time.Local)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), parsedTime, 25*time.Hour)
}

func TestLogAddPrefixes(t *testing.T) {
	b := &bytes.Buffer{}
	log.SetOutput(b)
	defer log.SetOutput(os.Stdout)
	defer DefaultLogger.SetLogLevel(LogLevelNothing)

	DefaultLogger.SetLogLevel(LogLevelDebug)

	// single prefix
	prefixLogger := DefaultLogger.WithPrefix("prefix")
	prefixLogger.Debugf("debug1")
	require.Contains(t, b.String(), "prefix")
	require.Contains(t, b.String(), "debug1")

	// multiple prefixes
	b.Reset()
	prefixLogger1 := DefaultLogger.WithPrefix("prefix1")
	prefixLogger2 := prefixLogger1.WithPrefix("prefix2")
	prefixLogger2.Debugf("debug2")
	require.Contains(t, b.String(), "prefix1")
	require.Contains(t, b.String(), "prefix2")
	require.Contains(t, b.String(), "debug2")
}

// nol8 (D106): QUIC_GO_LOG_LEVEL doesn't set the level, and an invalid value
// writes nothing. The level is set in init, so each value is checked in a
// child process that starts with the variable set.
func TestLogLevelNotFromEnv(t *testing.T) {
	const env = "QUIC_GO_LOG_LEVEL"
	if value, ok := os.LookupEnv(env); ok {
		require.Equal(t, LogLevelNothing, DefaultLogger.(*defaultLogger).logLevel, "%s=%s", env, value)
		return
	}
	for _, value := range []string{"DEBUG", "debug", "INFO", "ERROR", "asdf"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLogLevelNotFromEnv$")
		cmd.Env = append(os.Environ(), env+"="+value)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s=%s: %s", env, value, out)
		require.NotContains(t, string(out), "log level", "%s=%s", env, value)
	}
}
