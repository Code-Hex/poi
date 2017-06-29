package poi

import (
	"bytes"
	"fmt"
	"os"

	"reflect"

	flags "github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
)

// Options struct for parse command line arguments
type Options struct {
	Help       bool   `short:"h" long:"help" description:"show this message"`
	Version    bool   `short:"v" long:"version" description:"print the version"`
	Filename   string `short:"f" long:"file" required:"true" description:"specify a file of ltsv format access log"`
	Sortby     string `long:"sort-by" default:"count,desc" description:"specify a format like 'label,order' for sorting"`
	LabelAs    string `long:"label-as" description:"specify a yaml file with key and value for access log"` // yaml filename
	StackTrace bool   `long:"trace" description:"display detail error messages"`
}

func (opts *Options) parse(argv []string) ([]string, error) {
	p := flags.NewParser(opts, flags.None)
	args, err := p.ParseArgs(argv)
	if err != nil {
		os.Stderr.Write(opts.usage())
		return nil, errors.Wrap(err, "invalid command line options")
	}
	return args, nil
}

func (opts Options) usage() []byte {
	buf := bytes.Buffer{}
	fmt.Fprintf(&buf, `%s
Usage: %s [options]
Options:
`, msg, name)

	t := reflect.TypeOf(opts)
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag
		desc := tag.Get("description")
		var o string
		if s := tag.Get("short"); s != "" {
			o = fmt.Sprintf("-%s, --%s", tag.Get("short"), tag.Get("long"))
		} else {
			o = fmt.Sprintf("--%s", tag.Get("long"))
		}
		fmt.Fprintf(&buf, "  %-21s %s\n", o, desc)

		if deflt := tag.Get("default"); deflt != "" {
			indent := "        "
			fmt.Fprintf(&buf, "  %-21s default: --%s='%s'\n", indent, tag.Get("long"), deflt)
		}
	}

	return buf.Bytes()
}
