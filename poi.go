package poi

import (
	"fmt"
	"net/url"
	"os"

	"strconv"

	"sort"

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
	dataMap map[string]*data
)

func init() {
	/* We need these keys
	COUNT:
	MIN:
	MAX:
	AVG:
	P10:
	P50:
	P90:
	P95:
	P99:
	MAX(BODY):
	MIN(BODY):
	AVG(BODY):
	METHOD:
	URI:
	2xx:
	3xx:
	4xx:
	5xx:
	*/

	dataMap = make(map[string]*data)
}

func (p *poi) analyze() error {
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
		renderTable()
		l++
	}
	return nil
}

func renderTable() {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"count", "min", "max", "avg",
		"p10", "p50", "p90", "p95", "p99",
		"bodymin", "bodymax", "bodyavg",
		"method", "uri",
	})

	data := make([][]string, len(dataMap))
	for key, val := range dataMap {
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
	if _, ok := dataMap[key]; !ok {
		dataMap[key] = &data{
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
		}
	} else {
		dataMap[key].count++
		// Current response time
		dataMap[key].responseTimes = append(dataMap[key].responseTimes, resTime)
		sort.Float64s(dataMap[key].responseTimes)

		p10idx := getPercentileIdx(dataMap[key].count, 10)
		p50idx := getPercentileIdx(dataMap[key].count, 50)
		p90idx := getPercentileIdx(dataMap[key].count, 90)
		p95idx := getPercentileIdx(dataMap[key].count, 95)
		p99idx := getPercentileIdx(dataMap[key].count, 99)

		dataMap[key].p10 = dataMap[key].responseTimes[p10idx]
		dataMap[key].p50 = dataMap[key].responseTimes[p50idx]
		dataMap[key].p90 = dataMap[key].responseTimes[p90idx]
		dataMap[key].p95 = dataMap[key].responseTimes[p95idx]
		dataMap[key].p99 = dataMap[key].responseTimes[p99idx]

		if dataMap[key].maxTime < resTime {
			dataMap[key].maxTime = resTime
		}
		if dataMap[key].minTime > resTime || dataMap[key].minTime == 0 {
			dataMap[key].minTime = resTime
		}
		now := float64(dataMap[key].count)
		before := now - 1.0

		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dataMap[key].avgTime = (dataMap[key].avgTime*before + resTime) / now

		// Current response body size
		if dataMap[key].maxBody < bodySize {
			dataMap[key].maxBody = bodySize
		}
		if dataMap[key].minBody > bodySize || dataMap[key].minBody == 0 {
			dataMap[key].minBody = bodySize
		}
		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dataMap[key].avgBody = (dataMap[key].avgBody*before + bodySize) / now
	}

	// Current status code
	switch statusCode[0] {
	case '2':
		dataMap[key].code2xx++
	case '3':
		dataMap[key].code3xx++
	case '4':
		dataMap[key].code4xx++
	case '5':
		dataMap[key].code5xx++
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
