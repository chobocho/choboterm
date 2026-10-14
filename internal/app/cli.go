package app

import (
	"fmt"
	"strings"
	"time"

	"choboterm/frontend"
	"choboterm/internal/web"
)

// cliOptions are the command line options.
type cliOptions struct {
	web      bool   // start the web server (--web or --web=ADDR)
	addr     string // where it listens; "" = 127.0.0.1:7681
	headless bool   // no window: only the web server
	cert     string // TLS certificate and key files
	key      string
	insecure bool          // plain HTTP even when other PCs can connect
	open     bool          // open the page in this PC's browser
	noToken  bool          // disable web login token (Termux build only)
	grace    time.Duration // how long a disconnected page keeps its tabs
	help     bool
	// unknown are arguments that aren't ours. The window ignores them, as it
	// always did (wails dev passes its own -flags); --headless refuses them.
	unknown []string
}

const usage = `
Usage: choboterm [options]

  --web[=ADDR]     also serve the window's page to browsers (default 127.0.0.1:7681,
                   this PC only; 0.0.0.0:7681 lets other PCs in, over HTTPS)
  --headless       no window, only the web server (implies --web)
  --cert FILE      TLS certificate (PEM); default: a self-signed one, made when
  --key FILE       other PCs can connect
  --insecure       plain HTTP even when other PCs can connect (not recommended)
  --open           open the page in this PC's browser
  --grace DURATION how long a disconnected page keeps its tabs (default 10m)
  -h, --help       show this help

The page asks for the access token on first use; the URL printed at start
(and shown in Settings > Web) carries it.
`

const termuxUsage = `  --no-token       disable browser token login (Termux build only; unsafe on
                   networks you do not trust)
`

func usageText() string {
	if frontend.Termux {
		return strings.Replace(usage, "  -h, --help", termuxUsage+"  -h, --help", 1)
	}
	return usage
}

// parseArgs reads the command line (without the program name).
func parseArgs(args []string) (cliOptions, error) {
	var o cliOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, val, hasVal := strings.Cut(arg, "=")
		// value takes the option's value from "--opt=v" or the next argument.
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "--web", "-web":
			o.web = true
			if hasVal {
				o.addr = val
			}
		case "--headless", "-headless":
			o.headless, o.web = true, true
		case "--cert", "-cert":
			o.cert, err = value()
		case "--key", "-key":
			o.key, err = value()
		case "--insecure", "-insecure":
			o.insecure = true
		case "--no-token":
			if !frontend.Termux {
				return o, fmt.Errorf("--no-token is only supported in the Termux build")
			}
			o.noToken, o.web = true, true
		case "--open", "-open":
			o.open = true
		case "--grace", "-grace":
			var s string
			if s, err = value(); err == nil {
				if o.grace, err = time.ParseDuration(s); err == nil && o.grace <= 0 {
					err = fmt.Errorf("--grace must be positive")
				}
			}
		case "-h", "--help", "-help", "/?":
			o.help = true
		default:
			o.unknown = append(o.unknown, arg)
		}
		if err != nil {
			return o, err
		}
	}
	if (o.cert == "") != (o.key == "") {
		return o, fmt.Errorf("--cert and --key go together")
	}
	return o, nil
}

// webOptions returns the web server options asked for, or nil for none.
func (o cliOptions) webOptions() *web.Options {
	if !o.web {
		return nil
	}
	return &web.Options{Addr: o.addr, CertFile: o.cert, KeyFile: o.key, Insecure: o.insecure, NoToken: o.noToken, Grace: o.grace}
}
