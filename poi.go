package poi

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"sort"
	"strconv"

	"sync"

	"github.com/Code-Hex/exit"
	termbox "github.com/nsf/termbox-go"

	"github.com/hpcloud/tail"
	"github.com/pkg/errors"
)

// Poi is main struct for command line
type Poi struct {
	Options
	Label

	// window size
	width, height int

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
	dataMap.rownum = len(dataMap.keys)
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

	once := new(sync.Once)
	ctx, cancel := context.WithCancel(context.Background())

	defer func() {
		once.Do(cancel)
		termbox.Close()
	}()

	termbox.SetInputMode(termbox.InputEsc)

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
					p.renderMiddleLine()
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
						middle := p.height/2 - 1
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
	})
	p.curLine++
}
