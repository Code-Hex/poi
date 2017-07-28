package poi

import termbox "github.com/nsf/termbox-go"

const (
	foreground = termbox.ColorWhite
	background = termbox.ColorBlack
)

var topPane = true

func switchPane() {
	topPane = !topPane
}

func (p *Poi) clearLine(y int) {
	for i := 0; i < p.width; i++ {
		termbox.SetCell(i, y, 0, foreground, background)
	}
}

func renderStr(x, y int, str string) {
	renderStrWithColor(x, y, str, foreground, background)
}

func renderStrWithColor(x, y int, str string, fg, bg termbox.Attribute) {
	for i, c := range str {
		termbox.SetCell(x+i, y, c, fg, bg)
	}
}

func (p *Poi) clearPane(isTopPane bool) {
	half := p.height / 2
	if isTopPane {
		for x := 0; x < p.width; x++ {
			for y := 0; y < half; y++ {
				termbox.SetCell(x, y, 0, foreground, background)
			}
		}
	} else {
		for x := 0; x < p.width; x++ {
			for y := half + 1; y < p.height; y++ {
				termbox.SetCell(x, y, 0, foreground, background)
			}
		}
	}
}

func (p *Poi) fetchTermSize() {
	mu.Lock()
	p.width, p.height = termbox.Size()
	mu.Unlock()
}

func (p *Poi) flush() {
	mu.Lock()
	termbox.Flush()
	mu.Unlock()
}
