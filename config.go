package gocode

import (
	"sync/atomic"

	"sync"
)

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	proposeBuiltins    uint32 `json:"propose-builtins"`
	autobuild          uint32 `json:"autobuild"`
	unimportedPackages uint32 `json:"unimported-packages"`
	libPath            string `json:"lib-path"`
	forceDebugOutput   string `json:"force-debug-output"`
	mu                 sync.RWMutex

	// Excludes: PackageLookupMode, used to enable 'gb' lookup.
}

func (c *config) ProposeBuiltins() (b bool) {
	return atomic.LoadUint32(&c.proposeBuiltins) == 1
}

func (c *config) SetProposeBuiltins(b bool) {
	c.storeBool(&c.proposeBuiltins, b)
}

func (c *config) Autobuild() (b bool) {
	return atomic.LoadUint32(&c.autobuild) == 1
}

func (c *config) SetAutoBuild(b bool) {
	c.storeBool(&c.autobuild, b)
}

func (c *config) UnimportedPackages() bool {
	return atomic.LoadUint32(&c.unimportedPackages) == 1
}

func (c *config) SetUnimportedPackages(b bool) {
	c.storeBool(&c.unimportedPackages, b)
}

func (c *config) storeBool(addr *uint32, b bool) {
	n := uint32(0)
	if b {
		n = 1
	}
	atomic.StoreUint32(addr, n)
}

func (c *config) LibPath() (s string) {
	c.mu.RLock()
	s = c.libPath
	c.mu.RUnlock()
	return
}

func (c *config) SetLibPath(s string) {
	c.mu.Lock()
	c.libPath = s
	c.mu.Unlock()
	return
}

func (c *config) ForceDebugOutput() (s string) {
	c.mu.RLock()
	s = c.forceDebugOutput
	c.mu.RUnlock()
	return
}

var g_config = config{
	proposeBuiltins:    0,
	autobuild:          0,
	unimportedPackages: 0,
	libPath:            "",
	forceDebugOutput:   "",
}
