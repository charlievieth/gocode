#!/usr/bin/env bash

set -euo pipefail

TOOLS=(
    go
    goimports
    grep
    sed
)
for tool in "${TOOLS[@]}"; do
    command -v "${tool}" >/dev/null || {
        echo >&2 "error: missing required tool:" "${tool}"
        exit 1
    }
done

# exclude builtin, cmd, and internal packages
EXCLUDE='(^|/)(builtin|cmd|internal)(/|$)'

# convert each line into an underscore import: `fmt` => `   _ "fmt"`
QUOTE='s/\(.*\)/\t_ "\1"/g'

GOROOT="$(go env GOROOT)"
STDLIB="$(go list "${GOROOT}/src/..." | \grep -vE "${EXCLUDE}" | \sed "${QUOTE}")"

cat >./tmp.all.go <<-EOF
//go:build never

// all: import all stdlib packages for testing
package main

import (
${STDLIB}
)

var start = time.Now()

func main() {
    runtime.GC()
    var ms runtime.MemStats
    runtime.ReadMemStats(&ms)
    f, err := os.Create("all.mem.prof")
    if err != nil {
        log.Fatal(err)
    }
    if err := pprof.WriteHeapProfile(f); err != nil {
        log.Fatalf("WriteHeapProfile: %v", err)
    }
    log.Println(time.Since(start))
}
EOF

# use goimports to fixup "_" imports that we actually use
goimports -w ./tmp.all.go

# make sure the generated file compiles
go build -o /dev/null ./tmp.all.go

# replace ".Since(start)" with "@" for testing
sed -i 's/\.Since(start)/\.\@/g' ./tmp.all.go

mv ./tmp.all.go ./all.go
