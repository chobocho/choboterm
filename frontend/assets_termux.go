//go:build termux

package frontend

import "embed"

//go:embed all:dist-termux
var Assets embed.FS

const AssetDir = "dist-termux"
const Termux = true
