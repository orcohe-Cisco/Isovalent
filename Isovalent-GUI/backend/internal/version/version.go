// Package version carries the build identity of this binary.
//
// It exists so that "am I actually running the build I just pushed?" is a
// question the UI can answer. Stale images behind a pinned tag are the single
// most common way a Kubernetes deploy looks broken when nothing is wrong with
// the code, so the version is surfaced on /healthz and in the version panel.
package version

// Set at build time:
//
//	go build -ldflags "-X .../internal/version.Version=0.3.0 -X .../internal/version.Commit=$(git rev-parse --short HEAD)"
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String renders the full build identity, e.g. "0.3.0 (a1b2c3d, 2026-08-18)".
func String() string {
	s := Version
	if Commit != "unknown" && Commit != "" {
		s += " (" + Commit
		if Date != "unknown" && Date != "" {
			s += ", " + Date
		}
		s += ")"
	}
	return s
}
