package gocode

import "sync"

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	proposeBuiltins    bool
	libPath            string
	autobuild          bool
	forceDebugOutput   string
	unimportedPackages bool
	mu                 sync.RWMutex

	// Excludes: PackageLookupMode, used to enable 'gb' lookup.
}

func (c *config) UnimportedPackages() bool {
	c.mu.RLock()
	b := c.unimportedPackages
	c.mu.RUnlock()
	return b
}

func (c *config) SetUnimportedPackages(b bool) {
	c.mu.Lock()
	c.unimportedPackages = b
	c.mu.Unlock()
}

func (c *config) ProposeBuiltins() (b bool) {
	c.mu.RLock()
	b = c.proposeBuiltins
	c.mu.RUnlock()
	return
}

func (c *config) SetProposeBuiltins(b bool) {
	c.mu.Lock()
	c.proposeBuiltins = b
	c.mu.Unlock()
}

func (c *config) SetAutoBuild(b bool) {
	c.mu.Lock()
	c.autobuild = b
	c.mu.Unlock()
}

func (c *config) LibPath() (s string) {
	c.mu.RLock()
	s = c.libPath
	c.mu.RUnlock()
	return
}

func (c *config) Autobuild() (b bool) {
	c.mu.RLock()
	b = c.autobuild
	c.mu.RUnlock()
	return
}

func (c *config) ForceDebugOutput() (s string) {
	c.mu.RLock()
	s = c.forceDebugOutput
	c.mu.RUnlock()
	return
}

var g_config = config{
	proposeBuiltins:  false,
	libPath:          "",
	autobuild:        false,
	forceDebugOutput: "",
	mu:               sync.RWMutex{},
}
