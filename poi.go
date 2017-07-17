package poi

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"os"
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

// Poi is main struct for command line
type Poi struct {
	Options
	Label
	// relate with uri data
	header []string
	uriMap map[string]bool
	// logged for row number
	row int
	mu  sync.Mutex
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
	skip       error
	dataMap    *dict
	posXlist   []int
	headerPosY int
)

// New return pointered "poi" struct
func New() *Poi {
	return &Poi{
		row:    1,
		uriMap: make(map[string]bool),
	}
}

func (p *Poi) init() {
	p.header = make([]string, 0, 15)

	p.header = append(p.header,
		"COUNT",
		"MIN", "MAX", "AVG",
		"STDEV",
	)

	if p.Expand {
		p.header = append(p.header,
			"P10", "P50", "P90", "P95", "P99",
		)
	}

	p.header = append(p.header,
		"BODYMIN", "BODYMAX", "BODYAVG",
		"METHOD", "URI",
	)

	// start header position
	headerPosY = 4

	// allocate for header
	posXlist = make([]int, len(p.header), len(p.header))

	// dataMap is global
	dataMap = newDict()
}

func (p *Poi) analyze() error {
	p.init()
	if p.TailMode {
		return p.tailmode()
	}
	return p.normalmode()
}

func (p *Poi) normalmode() error {
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

func (p *Poi) rowInc() {
	p.mu.Lock()
	p.row++
	p.mu.Unlock()
}

func (p *Poi) getRow() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.row
}

func (p *Poi) tailmode() error {
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

	go p.monitorKeys(ctx, cancel, once)

tail:
	for ; ; p.rowInc() {
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
				return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", p.getRow())))
			}
			p.renderLikeTop()
		}
	}
	return nil
}

func (p *Poi) monitorKeys(ctx context.Context, cancel func(), once *sync.Once) {
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
				// special keys
				switch ev.Key {
				case termbox.KeyEsc, termbox.KeyCtrlC:
					once.Do(cancel)
				case termbox.KeyArrowUp:
					if dataMap.start > 0 {
						dataMap.start--
					}
					p.renderLikeTop()
				case termbox.KeyArrowDown:
					if dataMap.start+dataMap.rownum < len(dataMap.keys) {
						dataMap.start++
					}
					p.renderLikeTop()
				}
			case termbox.EventResize:
				p.renderLikeTop()
			case termbox.EventError:
				panic(ev.Err)
			}
		}
	}
}

func renderMiddleLine(width, height int) {
	half := height / 2

	if semihalf := (half - 1) - 4; semihalf < len(dataMap.keys) {
		dataMap.rownum = semihalf
	} else {
		dataMap.start = 0
		dataMap.rownum = len(dataMap.keys)
	}

	for i := 0; i < width; i++ {
		termbox.SetCell(i, half, '-', termbox.ColorDefault, termbox.ColorDefault)
	}
}

func (p *Poi) renderLikeTop() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

	read := 0 // Number of rows could be read

	// To adjust width
	countStrMaxLen := 0
	minBodyStrMaxLen := 6 // "BODYMIN" length is 6
	maxBodyStrMaxLen := 6 // "BODYMAX" length is 6
	avgBodyStrMaxLen := 6 // "BODYAVG" length is 6

	for _, key := range dataMap.keys {
		val := dataMap.get(key)
		read += val.count // Added number of rows

		countStr := fmt.Sprintf("%d", val.count)
		if l := len(countStr); l > countStrMaxLen {
			countStrMaxLen = l
		}

		minBStr := fmt.Sprintf("%.2f", val.minBody)
		if l := len(minBStr); l > minBodyStrMaxLen {
			minBodyStrMaxLen = l
		}

		maxBStr := fmt.Sprintf("%.2f", val.maxBody)
		if l := len(maxBStr); l > maxBodyStrMaxLen {
			maxBodyStrMaxLen = l
		}

		avgBStr := fmt.Sprintf("%.2f", val.avgBody)
		if l := len(avgBStr); l > avgBodyStrMaxLen {
			avgBodyStrMaxLen = l
		}
	}

	// Number of rows could not be read
	line := p.getRow()
	ignore := line - read

	renderStr(0, 0, fmt.Sprintf("Total URI number: %d", len(p.uriMap)))
	renderStr(0, 1, fmt.Sprintf("Read lines: %d, Ignore lines: %d", line, ignore))

	// Get width to draw the data
	for i, h := range p.header {
		switch h {
		case "COUNT":
			var base int
			if countStrMaxLen > 5 {
				base = countStrMaxLen
			} else {
				base = 5
			}
			posXlist[1] = base + 2 // for "MIN"
			renderStr(0, headerPosY, p.header[0])
		case "MIN":
			renderStr(posXlist[1], headerPosY, p.header[1])
		case "MAX", "AVG", "STDEV":
			posXlist[i] = posXlist[i-1] + 5 + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "P10", "P50", "P90", "P95", "P99":
			posXlist[i] = posXlist[i-1] + 5 + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "BODYMIN":
			posXlist[i] = posXlist[i-1] + 5 + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "BODYMAX":
			posXlist[i] = posXlist[i-1] + minBodyStrMaxLen + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "BODYAVG":
			posXlist[i] = posXlist[i-1] + avgBodyStrMaxLen + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "METHOD":
			posXlist[i] = posXlist[i-1] + avgBodyStrMaxLen + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		case "URI":
			posXlist[i] = posXlist[i-1] + 6 + 2
			renderStr(posXlist[i], headerPosY, p.header[i])
		}
	}
	p.renderData()
}

func (p *Poi) renderData() {
	// Rendering middle line
	renderMiddleLine(termbox.Size())

	// Rendering main data
	for i, key := range dataMap.sortedKeys(p.Sortby) {
		val := dataMap.get(key)
		sep := strings.Split(key, ":")
		uri, method := sep[0], sep[1]

		posY := (headerPosY + 1) + i

		clearLine(posY)

		renderStr(posXlist[0], posY, fmt.Sprintf("%d", val.count))
		renderStr(posXlist[1], posY, fmt.Sprintf("%.3f", val.minTime)) // Strlen is 5 <- "0.000"
		renderStr(posXlist[2], posY, fmt.Sprintf("%.3f", val.maxTime)) // Strlen is 5 <- "0.000"
		renderStr(posXlist[3], posY, fmt.Sprintf("%.3f", val.avgTime)) // Strlen is 5 <- "0.000"
		renderStr(posXlist[4], posY, fmt.Sprintf("%.3f", val.stdev))   // Strlen is 5 <- "0.000"
		if p.Expand {
			renderStr(posXlist[5], posY, fmt.Sprintf("%.3f", val.p10))      // Strlen is 5 <- "0.000"
			renderStr(posXlist[6], posY, fmt.Sprintf("%.3f", val.p50))      // Strlen is 5 <- "0.000"
			renderStr(posXlist[7], posY, fmt.Sprintf("%.3f", val.p90))      // Strlen is 5 <- "0.000"
			renderStr(posXlist[8], posY, fmt.Sprintf("%.3f", val.p95))      // Strlen is 5 <- "0.000"
			renderStr(posXlist[9], posY, fmt.Sprintf("%.3f", val.p99))      // Strlen is 5 <- "0.000"
			renderStr(posXlist[10], posY, fmt.Sprintf("%.2f", val.minBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[11], posY, fmt.Sprintf("%.2f", val.maxBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[12], posY, fmt.Sprintf("%.2f", val.avgBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[13], posY, method)                           // "METHOD" len is 6"
			renderStr(posXlist[14], posY, uri)
		} else {
			renderStr(posXlist[5], posY, fmt.Sprintf("%.2f", val.minBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[6], posY, fmt.Sprintf("%.2f", val.maxBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[7], posY, fmt.Sprintf("%.2f", val.avgBody)) // Strlen is 5 <- "00.00"
			renderStr(posXlist[8], posY, method)                           // "METHOD" length is 6"
			renderStr(posXlist[9], posY, uri)
		}
	}
	termbox.Flush()
}

func (p *Poi) renderTable() {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(p.header)

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
			tmp = append(tmp,
				fmt.Sprintf("%.3f", val.p10),
				fmt.Sprintf("%.3f", val.p50),
				fmt.Sprintf("%.3f", val.p90),
				fmt.Sprintf("%.3f", val.p95),
				fmt.Sprintf("%.3f", val.p99),
			)
		}

		tmp = append(tmp,
			fmt.Sprintf("%.2f", val.minBody),
			fmt.Sprintf("%.2f", val.maxBody),
			fmt.Sprintf("%.2f", val.avgBody),
			method, uri,
		)

		data = append(data, tmp)
	}
	table.AppendBulk(data)
	table.Render()
}

func (p *Poi) makeResult(tmp map[string]string) error {
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

func clearLine(y int) {
	width, _ := termbox.Size()
	for i := 0; i < width; i++ {
		termbox.SetCell(i, y, 0, termbox.ColorDefault, termbox.ColorDefault)
	}
}

func renderStr(x, y int, str string) {
	renderStrWithColor(x, y, str, termbox.ColorDefault, termbox.ColorDefault)
}

func renderStrWithColor(x, y int, str string, fg, bg termbox.Attribute) {
	for i, c := range str {
		termbox.SetCell(x+i, y, c, fg, bg)
	}
}
