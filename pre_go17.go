// +build !go1.7

package gocode

func init() {
	knownPackageIdents["context"] = "golang.org/x/net/context"
}
