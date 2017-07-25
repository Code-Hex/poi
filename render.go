package poi

import (
	"fmt"
	"os"
	"strings"

	termbox "github.com/nsf/termbox-go"
	"github.com/olekukonko/tablewriter"
)

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

func (p *Poi) renderAll() {
	p.fetchTermSize()
	p.renderTopPane()
	p.renderMiddleLine()
	p.renderBottomPane()
}

func (p *Poi) renderBottomPane() {
	clearPane(false)

	posMiddle := p.height / 2

	l := len(p.lineData)
	digit := len(fmt.Sprintf("%d", l))

	rowNum := p.curLine
	if h := p.height - 1 - (posMiddle + 1); p.curLine >= l-h {
		rowNum = l - h
	}

	// for rendering data
	d := p.lineData[p.curLine-1]
	l, idx := len(d.sortedKeys), p.dataIdx

	spaces := digit + 3

	// render
	for y := posMiddle + 1; y < p.height; y, rowNum = y+1, rowNum+1 {
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

func (p *Poi) renderMiddleLine() {
	whalf, hhalf := p.width/2, p.height/2

	if topPane {
		for i := 0; i < p.width; i++ {
			if i < whalf {
				termbox.SetCell(i, hhalf, '-', termbox.ColorGreen, background)
			} else {
				termbox.SetCell(i, hhalf, '-', foreground, background)
			}
		}
	} else {
		for i := 0; i < p.width; i++ {
			if i < whalf {
				termbox.SetCell(i, hhalf, '-', foreground, background)
			} else {
				termbox.SetCell(i, hhalf, '-', termbox.ColorGreen, background)
			}
		}
	}
}

func (p *Poi) renderTopPane() {
	clearPane(true)

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

	hhalf := p.height / 2
	// 4 is lines + space lines + header line
	if semihalf := (hhalf - 1) - 4; semihalf < len(dataMap.keys) {
		dataMap.rownum = semihalf
	} else {
		dataMap.start = 0
		dataMap.rownum = len(dataMap.keys)
	}

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
