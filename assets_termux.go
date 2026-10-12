//go:build termux

package main

import "embed"

//go:embed all:frontend/dist-termux
var assets embed.FS

const assetDir = "frontend/dist-termux"
