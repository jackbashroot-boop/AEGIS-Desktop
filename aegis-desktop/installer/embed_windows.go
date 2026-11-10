//go:build windows

package main

import _ "embed"

//go:embed payload-windows.zip
var payloadZip []byte
