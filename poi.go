package poi

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"sync"

	"github.com/Code-Hex/exit"
	"github.com/hpcloud/tail"
	termbox "github.com/nsf/termbox-go"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
)

type poi struct {
	Options
	Label
	stdout io.Writer
	uriMap map[string]bool
}

type data struct {
	count                              int
	minTime, maxTime, avgTime          float64
	stdev                              float64
	p10, p50, p90, p95, p99            float64
	maxBody, minBody, avgBody          float64
	code2xx, code3xx, code4xx, code5xx int
	responseTimes                      []float64
}

var (
	skip    error
	header  []string
	dataMap *dict
)

func (p *poi) init() {
	header = []string{
		"COUNT",
		"MIN", "MAX", "AVG",
		"STDEV",
	}

	if p.Expand {
		header = append(header, []string{
			"P10", "P50", "P90", "P95", "P99",
		}...)
	}

	header = append(header, []string{
		"BODYMIN", "BODYMAX", "BODYAVG",
		"METHOD", "URI",
	}...)

	dataMap = newDict()
}

func (p *poi) analyze() error {
	p.init()
	if p.TailMode {
		return p.tailmode()
	}
	return p.normalmode()
}

func (p *poi) normalmode() error {
	b, err := ioutil.ReadFile(p.Filename)
	if err != nil {
		return exit.MakeIOErr(err)
	}

	sc := bufio.NewScanner(bytes.NewReader(b))
	for l := 1; sc.Scan(); l++ {
		data := parseLTSV(sc.Text())
		if err := p.makeResult(data); err != nil {
			return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", l)))
		}
	}
	if err := sc.Err(); err != nil {
		return exit.MakeSoftWare(errors.Wrap(err, "Failed to read file"))
	}
	p.renderTable()
	return nil
}

func (p *poi) tailmode() error {
	file, err := tail.TailFile(p.Filename, tailConfig())
	if err != nil {
		return exit.MakeIOErr(err)
	}

	if err := termbox.Init(); err != nil {
		return exit.MakeSoftWare(err)
	}
	termbox.SetInputMode(termbox.InputEsc)

	once := new(sync.Once)
	ctx, cancel := context.WithCancel(context.Background())

	defer func() {
		once.Do(cancel)
		termbox.Close()
	}()

	go monitorKeys(ctx, cancel, once)

tail:
	for l := 1; ; l++ {
		select {
		case <-ctx.Done():
			break tail
		case line := <-file.Lines:
			if line == nil {
				// wait again only select
				continue
			}
			if line.Err != nil {
				return exit.MakeIOErr(err)
			}
			data := parseLTSV(line.Text)
			if err := p.makeResult(data); err != nil {
				return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", l)))
			}
			p.renderLikeTop(l)
		}
	}
	return nil
}

func monitorKeys(ctx context.Context, cancel func(), once *sync.Once) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			switch ev := termbox.PollEvent(); ev.Type {
			case termbox.EventKey:
				if ev.Ch == 'q' {
					once.Do(cancel)
				}
				switch ev.Key {
				case termbox.KeyEsc, termbox.KeyCtrlC:
					once.Do(cancel)
				}
			case termbox.EventError:
				panic(ev.Err)
			}
		}
	}
}

func (p *poi) renderLikeTop(line int) {
	clear(p.stdout)

	read := 0
	for _, key := range dataMap.keys {
		val := dataMap.get(key)
		read += val.count
	}
	ignore := line - read

	fmt.Fprintf(p.stdout, "Total URI number: %d\n", len(p.uriMap))
	fmt.Fprintf(p.stdout, "Read lines: %d, Ignore lines: %d\n\n", line, ignore)

	// Rendering header
	for _, h := range header {
		fmt.Fprintf(p.stdout, "%-9s", h)
	}
	fmt.Fprintf(p.stdout, "\n")

	// Rendering main data
	for _, key := range dataMap.sortedKeys(p.Sortby) {
		val := dataMap.get(key)
		sep := strings.Split(key, ":")
		uri, method := sep[0], sep[1]
		fmt.Fprintf(
			p.stdout,
			"%-9d%-9.3f%-9.3f%-9.3f%-9.3f",
			val.count,
			val.minTime,
			val.maxTime,
			val.avgTime,
			val.stdev,
		)
		if p.Expand {
			fmt.Fprintf(
				p.stdout,
				"%-9.3f%-9.3f%-9.3f%-9.3f%-9.3f",
				val.p10,
				val.p50,
				val.p90,
				val.p95,
				val.p99,
			)
		}
		fmt.Fprintf(
			p.stdout,
			"%-9.2f%-9.2f%-9.2f%-9s%-9s\n",
			val.minBody,
			val.maxBody,
			val.avgBody,
			method, uri,
		)
	}
}

func (p *poi) renderTable() {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)

	data := make([][]string, len(dataMap.keys))
	for _, key := range dataMap.sortedKeys(p.Sortby) {
		val := dataMap.get(key)
		sep := strings.Split(key, ":")
		uri, method := sep[0], sep[1]
		tmp := []string{
			fmt.Sprintf("%d", val.count),
			fmt.Sprintf("%.3f", val.minTime),
			fmt.Sprintf("%.3f", val.maxTime),
			fmt.Sprintf("%.3f", val.avgTime),
			fmt.Sprintf("%.3f", val.stdev),
		}

		if p.Expand {
			tmp = append(tmp, []string{
				fmt.Sprintf("%.3f", val.p10),
				fmt.Sprintf("%.3f", val.p50),
				fmt.Sprintf("%.3f", val.p90),
				fmt.Sprintf("%.3f", val.p95),
				fmt.Sprintf("%.3f", val.p99),
			}...)
		}

		tmp = append(tmp, []string{
			fmt.Sprintf("%.2f", val.minBody),
			fmt.Sprintf("%.2f", val.maxBody),
			fmt.Sprintf("%.2f", val.avgBody),
			method, uri,
		}...)

		data = append(data, tmp)
	}
	table.AppendBulk(data)
	table.Render()
}

func (p *poi) makeResult(tmp map[string]string) error {
	u, ok := tmp["uri"]
	if !ok {
		return errors.New("Could not found uri label")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return skip
	}
	uri := parsed.Path

	// Added to count number of uri
	if _, ok := p.uriMap[uri]; !ok {
		p.uriMap[uri] = true
	}

	statusCode, ok := tmp[p.StatusLabel]
	if !ok {
		return errors.New("Could not found status label")
	}

	apptime, ok := tmp[p.ApptimeLabel]
	if !ok {
		return errors.New("Could not found apptime label")
	}

	resTime, err := strconv.ParseFloat(apptime, 64)
	if err != nil {
		var reqTime float64
		req, ok := tmp[p.ReqtimeLabel]
		if !ok {
			return errors.New("Could not found reqtime label")
		}
		reqTime, err = strconv.ParseFloat(req, 64)
		if err != nil {
			return skip
		}
		resTime = reqTime
	}

	size, ok := tmp[p.SizeLabel]
	if !ok {
		return errors.New("Could not found size label")
	}
	bodySize, err := strconv.ParseFloat(size, 64)
	if err != nil {
		return skip
	}

	method, ok := tmp[p.MethodLabel]
	if !ok {
		return errors.New("Could not found method label")
	}

	key := uri + ":" + method
	dict := dataMap.get(key)
	if dict == nil {
		dataMap.set(key, &data{
			count:         1,
			minTime:       resTime,
			maxTime:       resTime,
			avgTime:       resTime,
			stdev:         0,
			p10:           resTime,
			p50:           resTime,
			p90:           resTime,
			p95:           resTime,
			p99:           resTime,
			minBody:       bodySize,
			maxBody:       bodySize,
			avgBody:       bodySize,
			responseTimes: []float64{resTime},
		})
		dict = dataMap.get(key)
	} else {
		dict.count++
		// Current response time
		dict.responseTimes = append(dict.responseTimes, resTime)
		sort.Float64s(dict.responseTimes)

		p10idx := getPercentileIdx(dict.count, 10)
		p50idx := getPercentileIdx(dict.count, 50)
		p90idx := getPercentileIdx(dict.count, 90)
		p95idx := getPercentileIdx(dict.count, 95)
		p99idx := getPercentileIdx(dict.count, 99)

		dict.p10 = dict.responseTimes[p10idx]
		dict.p50 = dict.responseTimes[p50idx]
		dict.p90 = dict.responseTimes[p90idx]
		dict.p95 = dict.responseTimes[p95idx]
		dict.p99 = dict.responseTimes[p99idx]

		if dict.maxTime < resTime {
			dict.maxTime = resTime
		}
		if dict.minTime > resTime || dict.minTime == 0 {
			dict.minTime = resTime
		}
		now := float64(dict.count)
		before := now - 1.0

		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dict.avgTime = (dict.avgTime*before + resTime) / now

		// standard deviation
		// stdev = √[(1 / n - 1) * {Σ(xi - avg) ^ 2}]
		stdev := float64(0)
		for _, t := range dict.responseTimes {
			diff := t - dict.avgTime
			stdev += diff * diff
		}
		dict.stdev = math.Sqrt(stdev / before)

		// Current response body size
		if dict.maxBody < bodySize {
			dict.maxBody = bodySize
		}
		if dict.minBody > bodySize || dict.minBody == 0 {
			dict.minBody = bodySize
		}
		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dict.avgBody = (dict.avgBody*before + bodySize) / now
	}

	// Current status code
	switch statusCode[0] {
	case '2':
		dict.code2xx++
	case '3':
		dict.code3xx++
	case '4':
		dict.code4xx++
	case '5':
		dict.code5xx++
	}
	return nil
}

func parseLTSV(text string) map[string]string {
	len := len(text)
	tmp := make(map[string]string)
	for idx, pos := 0, 0; pos < len; pos++ {
		if text[pos] == ':' {
			key := text[idx:pos]
			idx = pos + 1
			// Read until next tab letter
			for pos < len && text[pos] != '\t' {
				pos++
			}
			tmp[key] = text[idx:pos]
			idx = pos + 1
		}
	}
	return tmp
}

func tailConfig() tail.Config {
	return tail.Config{
		MustExist: true,
		Poll:      true,
		Follow:    true,
	}
}

func getPercentileIdx(len int, n int) int {
	idx := (len * n / 100) - 1
	if idx < 0 {
		return 0
	}
	return idx
}

func clear(out io.Writer) error {
	cmd := exec.Command("clear")
	cmd.Stdout = out
	return cmd.Run()
}

func renderStr(x, y int, str string) {
	renderStrWithColor(x, y, str, termbox.ColorDefault, termbox.ColorDefault)
}

func renderStrWithColor(x, y int, str string, fg, bg termbox.Attribute) {
	for i, c := range str {
		termbox.SetCell(x+i, y, c, fg, bg)
	}
}
