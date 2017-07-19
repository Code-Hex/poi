package poi

import termbox "github.com/nsf/termbox-go"

const (
	foreground = termbox.ColorWhite
	background = termbox.ColorBlack
)

var topPane = true

func clearLine(y int) {
	width, _ := termbox.Size()
	for i := 0; i < width; i++ {
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

func clearPane() {
	width, height := termbox.Size()
	half := height / 2
	if topPane {
		for x := 0; x < width; x++ {
			for y := 0; y < half; y++ {
				termbox.SetCell(x, y, 0, foreground, background)
			}
		}
	} else {
		for x := 0; x < width; x++ {
			for y := half + 1; y < height; y++ {
				termbox.SetCell(x, y, 0, foreground, background)
			}
		}
	}
}
