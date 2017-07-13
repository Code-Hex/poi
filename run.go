package poi

import (
	"fmt"
	"os"

	"github.com/Code-Hex/exit"
	"github.com/pkg/errors"
)

const (
	version = "0.0.1"
	msg     = name + " v" + version + ", Yet another access log profiler for ltsv"
	name    = "poi"
)

func New() *poi {
	return &poi{
		stdout: os.Stdout,
		uriMap: make(map[string]bool),
	}
}

func (p *poi) Run() int {
	if e := p.run(); e != nil {
		exitCode, err := UnwrapErrors(e)
		if p.StackTrace {
			fmt.Fprintf(os.Stderr, "Error:\n  %+v\n", e)
		} else {
			fmt.Fprintf(os.Stderr, "Error:\n  %v\n", err)
		}
		return exitCode
	}
	return 0
}

func (p *poi) run() error {
	args, err := p.prepare()
	if err != nil {
		return err
	}
	if err := p.profile(args); err != nil {
		return err
	}
	return nil
}

func (p *poi) profile(args []string) error {
	// See, koi.go
	return p.analyze()
}

func (p *poi) prepare() ([]string, error) {
	args, err := parseOptions(&p.Options, os.Args[1:])
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse command line args")
	}
	if err := p.makeLabel(); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *poi) makeLabel() error {
	if p.LabelAs != "" {
		label, err := loadYAML(p.LabelAs)
		if err != nil {
			return exit.MakeSoftWare(err)
		}
		p.Label = label
	}

	if p.ApptimeLabel == "" {
		p.ApptimeLabel = "apptime"
	}
	if p.ReqtimeLabel == "" {
		p.ReqtimeLabel = "request_time"
	}
	if p.StatusLabel == "" {
		p.StatusLabel = "status"
	}
	if p.SizeLabel == "" {
		p.SizeLabel = "size"
	}
	if p.MethodLabel == "" {
		p.MethodLabel = "method"
	}
	if p.URILabel == "" {
		p.URILabel = "uri"
	}

	return nil
}

func parseOptions(opts *Options, argv []string) ([]string, error) {
	o, err := opts.parse(argv)
	if err != nil {
		return nil, exit.MakeDataErr(err)
	}
	if opts.Help {
		return nil, exit.MakeUsage(errors.New(string(opts.usage())))
	}
	if opts.Version {
		return nil, exit.MakeUsage(errors.New(msg))
	}
	return o, nil
}
