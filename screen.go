package poi

import termbox "github.com/nsf/termbox-go"

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
