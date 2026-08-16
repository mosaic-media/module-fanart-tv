// Command module-fanart-tv runs this module as its own process, for a Platform
// that hosts it out of process (platform#39, sdk#7).
//
// platform#39 is arranged around the property that crossing the process
// boundary must not change what a module author writes: the Capability below is
// the same plain Go value a Platform would link into its own binary, its
// provider roles are the same methods, and its tests run with no transport at
// all.
//
// This module builds and behaves identically whether or not this file is used.
// Nothing else here imports it, and fanarttv.New remains what a
// statically-composed Platform would call; the switch to out-of-process is a
// composition change in the Platform, not a change here.
//
// Two things a module author inherits by using host.Serve, both easy to trip
// over and neither obvious from this file:
//
//   - Nothing may be written to stdout. go-plugin writes its handshake there,
//     and anything else corrupts it. Use the Telemetry reached from the
//     invocation's context (sdk#5).
//   - The Caller is a handle, not a session. It is minted per invocation and
//     stops resolving when that invocation returns, so it cannot usefully be
//     stored. Module code forwards what it was given, exactly as platform#13
//     already required.
package main

import (
	"github.com/mosaic-media/sdk/host"

	fanarttv "github.com/mosaic-media/module-fanart-tv"
)

func main() {
	// nil takes the module's default HTTP client. Out of process the Platform
	// cannot hand one in — an *http.Client does not cross a process boundary — so
	// egress is contained by the forward proxy the Platform operates and forces
	// this process's default transport through (platform#39; sdk/host's egress path).
	host.Serve(fanarttv.New(nil))
}
