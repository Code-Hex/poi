package koi

import (
	"bytes"
	"fmt"
	"os"

	flags "github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
)

// Options struct for parse command line arguments
type Options struct {
	Help       bool   `short:"h" long:"help"`
	Version    bool   `short:"v" long:"version"`
	Sortby     string `long:"sort-by"`
	LabelAs    string `long:"label-as"` // yaml filename
	Filename   string `short:"f" long:"file" required:"true"`
	StackTrace bool   `long:"trace"`
}

func (opts *Options) parse(argv []string) ([]string, error) {
	p := flags.NewParser(opts, flags.PrintErrors)
	args, err := p.ParseArgs(argv)
	if err != nil {
		os.Stderr.Write(opts.usage())
		return nil, errors.Wrap(err, "invalid command line options")
	}
	return args, nil
}

func (opts Options) usage() []byte {
	buf := bytes.Buffer{}
	fmt.Fprintf(&buf, msg+
		`Usage: %s [options]
  Options:
  -h,  --help                print usage and exit
  -v,  --version             display the version of poi and exit
  --trace                    display detail error messages
`, name)
	return buf.Bytes()
}
