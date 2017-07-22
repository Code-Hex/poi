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
	termbox "github.com/nsf/termbox-go"

	"github.com/hpcloud/tail"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
)

// Poi is main struct for command line
type Poi struct {
	Options
	Label

	// Related with access log data
	header     []string
	posXlist   []int
	headerPosY int
	uriMap     map[string]bool
	lineData   []*data
	curLine    int
	dataIdx    int

	// Logged for row number
	row int
	mu  sync.Mutex
}

type data struct {
	sortedKeys []string
	data       map[string]string
	needLines  int
}

type tableData struct {
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
	dataMap *dict
)

// New return pointered "poi" struct
func New() *Poi {
	return &Poi{
		uriMap: make(map[string]bool),
	}
}

func (p *Poi) init() {
	// Allocate for header
	p.header = make([]string, 0, 15)

	// Allocate to store log lines on memory
	p.lineData = make([]*data, 0, p.Limit)

	// Start header position on the screen
	p.headerPosY = 4

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

	// Allocate for header
	p.posXlist = make([]int, len(p.header), len(p.header))

	// dataMap on the global
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
	for {
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

			// Line increment
			p.row++

			// send text
			data := parseLTSV(line.Text)
			if err := p.makeResult(data); err != nil {
				return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", p.row)))
			}
			p.setLineData(data)
			p.renderAll()
			p.flush()
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
				case termbox.KeyTab:
					switchPane()
					renderMiddleLine()
				case termbox.KeyEsc, termbox.KeyCtrlC:
					once.Do(cancel)
				case termbox.KeyArrowUp:
					if topPane {
						if dataMap.start > 0 {
							dataMap.startDec()
						}
						p.renderTopPane()
					} else {
						if p.dataIdx == 0 && p.curLine > 1 {
							p.curLine--
						} else if p.dataIdx > 0 {
							p.dataIdx--
						}
						p.renderBottomPane()
					}
				case termbox.KeyArrowDown:
					if topPane {
						if dataMap.start+dataMap.rownum < len(dataMap.keys) {
							dataMap.startInc()
						}
						p.renderTopPane()
					} else {
						_, height := termbox.Size()
						middle := height/2 - 1

						d := p.lineData[p.curLine-1]
						if l := len(d.sortedKeys); p.curLine < len(p.lineData) {
							if middle-p.dataIdx == l {
								p.curLine++
								p.dataIdx = 0
							} else if p.dataIdx < l {
								p.dataIdx++
							}
						}
						p.renderBottomPane()
					}
				}
			case termbox.EventResize:
				p.renderAll()
			case termbox.EventError:
				panic(ev.Err)
			}
			p.flush()
		}
	}
}

func (p *Poi) renderAll() {
	p.renderTopPane()
	switchPane()

	p.renderBottomPane()
	switchPane()
}

func (p *Poi) renderBottomPane() {
	clearPane()

	_, height := termbox.Size()
	posMiddle := height / 2

	l := len(p.lineData)
	digit := len(fmt.Sprintf("%d", l))

	rowNum := p.curLine
	if h := height - 1 - (posMiddle + 1); p.curLine >= l-h {
		rowNum = l - h
	}

	// for rendering data
	d := p.lineData[p.curLine-1]
	l, idx := len(d.sortedKeys), p.dataIdx

	spaces := digit + 3

	// render
	for y := posMiddle + 1; y < height; y, rowNum = y+1, rowNum+1 {
		clearLine(y)
		if p.curLine == rowNum {
			renderStrWithColor(0, y, fmt.Sprintf(" %*d ", digit, rowNum),
				termbox.ColorYellow,
				background,
			)
		} else {
			renderStrWithColor(0, y, fmt.Sprintf(" %*d ", digit, rowNum),
				termbox.ColorWhite,
				background,
			)
		}

		if idx < l {
			key := d.sortedKeys[idx]
			renderStr(spaces, y, key+" : "+d.data[key])
			idx++
		}
	}
}

func renderMiddleLine() {
	width, height := termbox.Size()
	whalf, hhalf := width/2, height/2

	// 4 is line that info line + space line + header line
	if semihalf := (hhalf - 1) - 4; semihalf < len(dataMap.keys) {
		dataMap.rownum = semihalf
	} else {
		dataMap.start = 0
		dataMap.rownum = len(dataMap.keys)
	}

	if topPane {
		for i := 0; i < whalf; i++ {
			termbox.SetCell(i, hhalf, '-', termbox.ColorGreen, background)
		}
		for i := whalf; i < width; i++ {
			termbox.SetCell(i, hhalf, '-', foreground, background)
		}
	} else {
		for i := 0; i < whalf; i++ {
			termbox.SetCell(i, hhalf, '-', foreground, background)
		}
		for i := whalf; i < width; i++ {
			termbox.SetCell(i, hhalf, '-', termbox.ColorGreen, background)
		}
	}
}

func (p *Poi) renderTopPane() {
	clearPane()

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
	ignore := p.row - read

	renderStr(0, 0, fmt.Sprintf("Total URI: %d", len(p.uriMap)))
	renderStr(0, 1, fmt.Sprintf("Read lines: %d, Ignore lines: %d", p.row, ignore))

	// Get width to draw data
	for i, h := range p.header {
		switch h {
		case "COUNT":
			var base int
			if countStrMaxLen > 5 {
				base = countStrMaxLen
			} else {
				base = 5
			}
			p.posXlist[1] = base + 2 // for "MIN"
			renderStr(0, p.headerPosY, p.header[0])
		case "MIN":
			renderStr(p.posXlist[1], p.headerPosY, p.header[1])
		case "MAX", "AVG", "STDEV":
			p.posXlist[i] = p.posXlist[i-1] + 5 + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "P10", "P50", "P90", "P95", "P99":
			p.posXlist[i] = p.posXlist[i-1] + 5 + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "BODYMIN":
			p.posXlist[i] = p.posXlist[i-1] + 5 + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "BODYMAX":
			p.posXlist[i] = p.posXlist[i-1] + minBodyStrMaxLen + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "BODYAVG":
			p.posXlist[i] = p.posXlist[i-1] + avgBodyStrMaxLen + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "METHOD":
			p.posXlist[i] = p.posXlist[i-1] + avgBodyStrMaxLen + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		case "URI":
			p.posXlist[i] = p.posXlist[i-1] + 6 + 2
			renderStr(p.posXlist[i], p.headerPosY, p.header[i])
		}
	}

	// Rendering middle line
	renderMiddleLine()

	// Rendering main data
	for i, key := range dataMap.sortedKeys(p.Sortby) {
		val := dataMap.get(key)
		sep := strings.Split(key, ":")
		uri, method := sep[0], sep[1]

		posY := (p.headerPosY + 1) + i

		clearLine(posY)

		renderStr(p.posXlist[0], posY, fmt.Sprintf("%d", val.count))
		renderStr(p.posXlist[1], posY, fmt.Sprintf("%.3f", val.minTime)) // Strlen is 5 <- "0.000"
		renderStr(p.posXlist[2], posY, fmt.Sprintf("%.3f", val.maxTime)) // Strlen is 5 <- "0.000"
		renderStr(p.posXlist[3], posY, fmt.Sprintf("%.3f", val.avgTime)) // Strlen is 5 <- "0.000"
		renderStr(p.posXlist[4], posY, fmt.Sprintf("%.3f", val.stdev))   // Strlen is 5 <- "0.000"
		if p.Expand {
			renderStr(p.posXlist[5], posY, fmt.Sprintf("%.3f", val.p10))      // Strlen is 5 <- "0.000"
			renderStr(p.posXlist[6], posY, fmt.Sprintf("%.3f", val.p50))      // Strlen is 5 <- "0.000"
			renderStr(p.posXlist[7], posY, fmt.Sprintf("%.3f", val.p90))      // Strlen is 5 <- "0.000"
			renderStr(p.posXlist[8], posY, fmt.Sprintf("%.3f", val.p95))      // Strlen is 5 <- "0.000"
			renderStr(p.posXlist[9], posY, fmt.Sprintf("%.3f", val.p99))      // Strlen is 5 <- "0.000"
			renderStr(p.posXlist[10], posY, fmt.Sprintf("%.2f", val.minBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[11], posY, fmt.Sprintf("%.2f", val.maxBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[12], posY, fmt.Sprintf("%.2f", val.avgBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[13], posY, method)                           // "METHOD" len is 6"
			renderStr(p.posXlist[14], posY, uri)
		} else {
			renderStr(p.posXlist[5], posY, fmt.Sprintf("%.2f", val.minBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[6], posY, fmt.Sprintf("%.2f", val.maxBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[7], posY, fmt.Sprintf("%.2f", val.avgBody)) // Strlen is 5 <- "00.00"
			renderStr(p.posXlist[8], posY, method)                           // "METHOD" length is 6"
			renderStr(p.posXlist[9], posY, uri)
		}
	}
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
		dataMap.set(key, &tableData{
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

func (p *Poi) setLineData(val map[string]string) {
	l := len(val)
	keys := make([]string, 0, l)
	for k := range val {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(p.lineData)+1 > p.Limit {
		p.lineData = p.lineData[1:] // Remove a head
	}
	p.lineData = append(p.lineData, &data{
		data:       val,
		sortedKeys: keys,
		needLines:  l,
	})
	p.curLine++
}
