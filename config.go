package gocode

import "sync"

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	proposeBuiltins  bool   `json:"propose-builtins"`
	libPath          string `json:"lib-path"`
	autobuild        bool   `json:"autobuild"`
	forceDebugOutput string `json:"force-debug-output"`
	mu               sync.RWMutex
}

func (c *config) ProposeBuiltins() (b bool) {
	c.mu.RLock()
	b = c.proposeBuiltins
	c.mu.RUnlock()
	return
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
