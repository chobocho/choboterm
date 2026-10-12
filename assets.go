//go:build !termux

package main

import "embed"

//go:embed all:frontend/dist
var assets embed.FS

const assetDir = "frontend/dist"
