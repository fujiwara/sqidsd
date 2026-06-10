package sqidsd

import (
	"context"
	"time"

	"github.com/alecthomas/kong"
)

// CLI defines command line options of sqidsd.
type CLI struct {
	Addresses       []string         `arg:"" name:"address" help:"Addresses to listen on. A unix domain socket path (e.g. /tmp/sqidsd.sock) or a TCP address (e.g. 127.0.0.1:8089) is detected automatically." env:"SQIDSD_ADDRESS"`
	Alphabet        string           `help:"Custom Sqids alphabet." env:"SQIDSD_ALPHABET"`
	MinLength       uint8            `name:"min-length" help:"Minimum length of generated Sqids IDs." env:"SQIDSD_MIN_LENGTH" default:"0"`
	ShutdownTimeout time.Duration    `name:"shutdown-timeout" help:"Grace period to drain connections on shutdown." env:"SQIDSD_SHUTDOWN_TIMEOUT" default:"5s"`
	Version         kong.VersionFlag `help:"Show version."`
}

// RunCLI parses command line arguments and runs the server until ctx is canceled.
func RunCLI(ctx context.Context, args []string) error {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("sqidsd"),
		kong.Description("A daemon to encode/decode Sqids IDs via line-oriented JSON-RPC 2.0."),
		kong.Vars{"version": Version},
	)
	if err != nil {
		return err
	}
	if _, err := parser.Parse(args); err != nil {
		return err
	}
	srv, err := New(&Options{
		Addresses:       cli.Addresses,
		Alphabet:        cli.Alphabet,
		MinLength:       cli.MinLength,
		ShutdownTimeout: cli.ShutdownTimeout,
	})
	if err != nil {
		return err
	}
	return srv.Run(ctx)
}
