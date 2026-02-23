//go:build tools

package golib

// This import ensures golang.org/x/mobile/bind stays in go.mod.
// gomobile bind requires it in the dependency graph but no source code
// imports it directly. The "tools" build tag prevents it from affecting
// normal builds.
import _ "golang.org/x/mobile/bind"
