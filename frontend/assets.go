//go:build !termux

package frontend

import "embed"

//go:embed all:dist
var Assets embed.FS

const AssetDir = "dist"
const Termux = false
