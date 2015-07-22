package gocode

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	ProposeBuiltins  bool   `json:"propose-builtins"`
	LibPath          string `json:"lib-path"`
	Autobuild        bool   `json:"autobuild"`
	ForceDebugOutput string `json:"force-debug-output"`
}

var g_config = config{
	ProposeBuiltins:  false,
	LibPath:          "",
	Autobuild:        false,
	ForceDebugOutput: "",
}
