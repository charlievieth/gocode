package gocode

import (
	"path/filepath"
	"sync"
)

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

var g_config config // global config

type config struct {
	proposeBuiltins    bool     `json:"propose-builtins"`
	libPath            string   `json:"lib-path"`
	pathList           []string `json:"path_list"`
	autobuild          bool     `json:"autobuild"`
	forceDebugOutput   string   `json:"force-debug-output"`
	unimportedPackages bool     `json:"unimported-packages"`
	mu                 sync.RWMutex

	// Excludes: PackageLookupMode, used to enable 'gb' lookup.
}

func (c *config) UnimportedPackages() (b bool) {
	c.mu.RLock()
	b = c.unimportedPackages
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

func (c *config) SetLibPath(s string) {
	c.mu.Lock()
	c.libPath = s
	c.pathList = filepath.SplitList(s)
	c.mu.Unlock()
	return
}

func (c *config) PathList() (a []string) {
	c.mu.RLock()
	a = c.pathList
	c.mu.RUnlock()
	return a
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
