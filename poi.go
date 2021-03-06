package poi

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"sort"
	"strconv"

	"golang.org/x/sync/errgroup"

	"sync"

	"github.com/Code-Hex/exit"
	termbox "github.com/nsf/termbox-go"

	"runtime"

	"github.com/hpcloud/tail"
	"github.com/pkg/errors"
)

var mu sync.RWMutex

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

	// Tasks
	count int
}

type data struct {
	sortedKeys []string
	data       map[string]string
}

type lineData struct {
	row int
	tmp map[string]string
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

type parsedLabel struct {
	uri, method string
	statusCode  string
	resTime     float64
	bodySize    float64
}

var (
	dataMap *dict
	skip    = makeSkipError()
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
		label, err := p.parseLabel(data)
		if err != nil {
			if _, ok := err.(skipErr); ok {
				continue
			}
			return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", l)))
		}
		p.makeResult(label)
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

	var otail sync.Once

	ncpu := runtime.NumCPU()

	flush := make(chan struct{})
	sendCh := make(chan lineData, ncpu*2)
	labelCh := make(chan *parsedLabel, ncpu*2)
	termbox.SetInputMode(termbox.InputEsc)

	var grp errgroup.Group
	defer termbox.Close()

	grp.Go(func() error {
		for range flush {
			p.renderAll()
			p.flush()
		}
		return nil
	})

	grp.Go(func() error {
		defer func() {
			otail.Do(func() {
				file.Stop()
			})
			close(sendCh)
		}()

		row := 0
		for line := range file.Lines {
			if line.Err != nil {
				return exit.MakeIOErr(line.Err)
			}
			// Line increment
			row++
			sendCh <- lineData{row, parseLTSV(line.Text)}
		}
		return nil
	})

	var (
		olabel sync.Once
		oflush sync.Once
	)
	for n := 0; n < ncpu; n++ {
		grp.Go(func() error {
			defer olabel.Do(func() { close(labelCh) })
			for line := range sendCh {
				p.setLineData(line.tmp) // This method to watch the log
				label, err := p.parseLabel(line.tmp)
				if err != nil {
					if _, ok := err.(skipErr); ok {
						continue
					}
					return exit.MakeSoftWare(errors.Wrap(err, fmt.Sprintf("at line: %d", line.row)))
				}
				p.row = line.row
				labelCh <- label
			}
			return nil
		})
	}

	grp.Go(func() error {
		defer oflush.Do(func() { close(flush) })
		for label := range labelCh {
			p.makeResult(label)
			flush <- struct{}{}
		}
		return nil
	})

	grp.Go(func() error {
		defer otail.Do(func() {
			file.Stop()
		})

	monitor:
		for {
			switch ev := termbox.PollEvent(); ev.Type {
			case termbox.EventKey:
				if ev.Ch == 'q' {
					break monitor
				}
				// special keys
				switch ev.Key {
				case termbox.KeyEsc, termbox.KeyCtrlC:
					break monitor
				case termbox.KeyTab:
					switchPane()
					p.renderMiddleLine()
				case termbox.KeyArrowUp:
					p.arrowUpAction()
				case termbox.KeyArrowDown:
					p.arrowDownAction()
				}
			case termbox.EventResize:
				p.renderAll()
			case termbox.EventError:
				return exit.MakeSoftWare(ev.Err)
			}
			p.flush()
		}
		return nil
	})

	return grp.Wait()
}

func (p *Poi) addTask() {
	mu.Lock()
	p.count++
	mu.Unlock()
}

func (p *Poi) doneTask() {
	mu.Lock()
	if p.count--; p.count < 0 {
		panic("tasks over decrement")
	}
	mu.Unlock()
}

func (p *Poi) isZeroTask() bool {
	mu.RLock()
	count := p.count
	mu.RUnlock()
	return count == 0
}

func (p *Poi) parseLabel(tmp map[string]string) (*parsedLabel, error) {
	u, ok := tmp["uri"]
	if !ok {
		return nil, errors.New("Could not found uri label")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, skip
	}
	uri := parsed.Path

	// Added to count number of uri
	if _, ok := p.uriMap[uri]; !ok {
		p.uriMap[uri] = true
	}

	statusCode, ok := tmp[p.StatusLabel]
	if !ok {
		return nil, errors.New("Could not found status label")
	}

	apptime, ok := tmp[p.ApptimeLabel]
	if !ok {
		return nil, errors.New("Could not found apptime label")
	}

	resTime, err := strconv.ParseFloat(apptime, 64)
	if err != nil {
		var reqTime float64
		req, ok := tmp[p.ReqtimeLabel]
		if !ok {
			return nil, errors.New("Could not found reqtime label")
		}
		reqTime, err = strconv.ParseFloat(req, 64)
		if err != nil {
			return nil, skip
		}
		resTime = reqTime
	}

	size, ok := tmp[p.SizeLabel]
	if !ok {
		return nil, errors.New("Could not found size label")
	}
	bodySize, err := strconv.ParseFloat(size, 64)
	if err != nil {
		return nil, skip
	}

	method, ok := tmp[p.MethodLabel]
	if !ok {
		return nil, errors.New("Could not found method label")
	}
	return &parsedLabel{
		uri:        uri,
		method:     method,
		statusCode: statusCode,
		resTime:    resTime,
		bodySize:   bodySize,
	}, nil
}

func (p *Poi) makeResult(l *parsedLabel) {
	key := l.uri + ":" + l.method
	dict := dataMap.get(key)
	if dict == nil {
		dataMap.set(key, &tableData{
			count:         1,
			minTime:       l.resTime,
			maxTime:       l.resTime,
			avgTime:       l.resTime,
			p10:           l.resTime,
			p50:           l.resTime,
			p90:           l.resTime,
			p95:           l.resTime,
			p99:           l.resTime,
			minBody:       l.bodySize,
			maxBody:       l.bodySize,
			avgBody:       l.bodySize,
			responseTimes: []float64{l.resTime},
		})
		dict = dataMap.get(key)
	} else {
		dict.count++
		// Current response time
		dict.responseTimes = append(dict.responseTimes, l.resTime)
		sort.Float64s(dict.responseTimes)

		// Get the index for percentile
		p10idx := getPercentileIdx(dict.count, 10)
		p50idx := getPercentileIdx(dict.count, 50)
		p90idx := getPercentileIdx(dict.count, 90)
		p95idx := getPercentileIdx(dict.count, 95)
		p99idx := getPercentileIdx(dict.count, 99)

		// Get percentiles
		dict.p10 = dict.responseTimes[p10idx]
		dict.p50 = dict.responseTimes[p50idx]
		dict.p90 = dict.responseTimes[p90idx]
		dict.p95 = dict.responseTimes[p95idx]
		dict.p99 = dict.responseTimes[p99idx]

		if dict.maxTime < l.resTime {
			dict.maxTime = l.resTime
		}
		if dict.minTime > l.resTime || dict.minTime == 0 {
			dict.minTime = l.resTime
		}
		now := float64(dict.count)
		before := now - 1.0

		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dict.avgTime = (dict.avgTime*before + l.resTime) / now

		// standard deviation
		// stdev = √[(1 / n - 1) * {Σ(xi - avg) ^ 2}]
		stdev := float64(0)
		for _, t := range dict.responseTimes {
			diff := t - dict.avgTime
			stdev += diff * diff
		}
		dict.stdev = math.Sqrt(stdev / before)

		// Current response body size
		if dict.maxBody < l.bodySize {
			dict.maxBody = l.bodySize
		}
		if dict.minBody > l.bodySize || dict.minBody == 0 {
			dict.minBody = l.bodySize
		}
		// newAvg = (oldAvg * lenOfoldAvg + newVal) / lenOfnewAvg
		dict.avgBody = (dict.avgBody*before + l.bodySize) / now
	}

	// Current status code
	switch l.statusCode[0] {
	case '2':
		dict.code2xx++
	case '3':
		dict.code3xx++
	case '4':
		dict.code4xx++
	case '5':
		dict.code5xx++
	}
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
		ReOpen:    true,
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
