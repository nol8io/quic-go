module test

go 1.26.0

// The version doesn't matter here, as we're replacing it with the currently checked out code anyway.
require github.com/nol8io/quic-go v0.21.0

require (
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

replace github.com/nol8io/quic-go => ../../
