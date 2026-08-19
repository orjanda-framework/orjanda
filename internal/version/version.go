// Package version reads version metadata from the running binary's build info.
package version

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
)

// Info holds version metadata read from the running binary's build info.
// See TAD §18.1 for the full contract.
type Info struct {
	ModulePath  string // debug.BuildInfo.Main.Path
	Version     string // debug.BuildInfo.Main.Version, verbatim
	IsRelease   bool   // true iff Version is a clean, tagged semver
	GoVersion   string // debug.BuildInfo.GoVersion
	VCSRevision string // from BuildInfo.Settings["vcs.revision"], if present
	VCSDirty    bool   // from BuildInfo.Settings["vcs.modified"], if present
}

// Current reads the running binary's own build metadata via
// runtime/debug.ReadBuildInfo(). Never makes a network call, never reads
// files, never shells out to git. Returns a zero Info on error (e.g. when
// build info is stripped).
// See TAD §18.1 for the full contract.
func Current() Info {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{}
	}

	vcsRevision := ""
	vcsDirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			vcsRevision = setting.Value
		case "vcs.modified":
			vcsDirty = setting.Value == "true"
		}
	}

	return Info{
		ModulePath:  info.Main.Path,
		Version:     info.Main.Version,
		IsRelease:   isRelease(info.Main.Version),
		GoVersion:   info.GoVersion,
		VCSRevision: vcsRevision,
		VCSDirty:    vcsDirty,
	}
}

// isRelease classifies a version string as a release or not.
// Returns true only for clean semver tags (e.g. v1.2.3), false for
// pseudo-versions, (devel), empty strings, or +dirty suffixes.
// See TAD §18.2 for the full rationale.
func isRelease(v string) bool {
	if v == "" || v == "(devel)" {
		return false
	}
	if strings.HasSuffix(v, "+dirty") {
		return false
	}
	return !module.IsPseudoVersion(v)
}
