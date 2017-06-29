package poi

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/Code-Hex/exit"
	"github.com/hpcloud/tail"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
)

type poi struct {
	Options
	Label
}

type data struct {
	count                              int
	minTime, maxTime, avgTime          float64
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

func init() {
	header = []string{
		"count",
		"min", "max", "avg",
		"p10", "p50", "p90", "p95", "p99",
		"bodymin", "bodymax", "bodyavg",
		"method", "uri",
	}
	dataMap = newDict()
}

func (p *poi) analyze() error {
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

	l := 1
	for sc.Scan() {
		data := parseLTSV(sc.Text())
		if err := p.makeResult(data); err != nil {
			return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", l)))
		}
		l++
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

	// Waiting channels
	l := 1
	for line := range file.Lines {
		if line.Err != nil {
			return exit.MakeIOErr(err)
		}
		data := parseLTSV(line.Text)
		if err := p.makeResult(data); err != nil {
			return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", l)))
		}
		clear(os.Stdout)
		p.renderTable()
		l++
	}
	return nil
}

func (p *poi) renderTable() {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)

	data := make([][]string, len(dataMap.keys))
	for _, key := range dataMap.sortedKeys(p.Sortby) {
		val := dataMap.get(key)
		sep := strings.Split(key, ":")
		uri, method := sep[0], sep[1]
		data = append(data, []string{
			fmt.Sprintf("%d", val.count),
			fmt.Sprintf("%.3f", val.minTime),
			fmt.Sprintf("%.3f", val.maxTime),
			fmt.Sprintf("%.3f", val.avgTime),
			fmt.Sprintf("%.3f", val.p10),
			fmt.Sprintf("%.3f", val.p50),
			fmt.Sprintf("%.3f", val.p90),
			fmt.Sprintf("%.3f", val.p95),
			fmt.Sprintf("%.3f", val.p99),
			fmt.Sprintf("%.2f", val.minBody),
			fmt.Sprintf("%.2f", val.maxBody),
			fmt.Sprintf("%.2f", val.avgBody),
			method, uri,
		})
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
