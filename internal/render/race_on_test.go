//go:build race

package render

// raceEnabled is whether the race detector is built in. Its
// instrumentation allocates on its own account, so the tests that count
// allocations skip under it; the ordinary run still counts them.
const raceEnabled = true
