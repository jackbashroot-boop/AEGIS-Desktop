//go:build linux

package main

import _ "embed"

//go:embed payload-linux.zip
var payloadZip []byte
