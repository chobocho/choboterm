// Package frontend holds what the Go program embeds of the page: the built
// page (dist, or dist-termux for the Termux build) and the English dictionary
// the Go side shares with it. They are embedded from here because go:embed
// only reaches files below its own folder.
package frontend
