// Package version tracks build metadata for the application.
package version

import (
	"sync"
)

// Info describes build metadata for the application.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

var (
	info      = Info{Version: "dev"}
	infoMutex sync.RWMutex
)

// Set updates the version metadata exposed by the application.
func Set(v Info) {
	infoMutex.Lock()
	defer infoMutex.Unlock()

	if v.Version == "" {
		v.Version = "dev"
	}
	info = v
}

// Current returns the currently configured build metadata.
func Current() Info {
	infoMutex.RLock()
	defer infoMutex.RUnlock()
	return info
}
