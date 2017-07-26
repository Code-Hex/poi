package poi

func (p *Poi) arrowUpAction() {
	if topPane {
		if dataMap.start > 0 {
			dataMap.start--
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
}

func (p *Poi) arrowDownAction() {
	if topPane {
		if dataMap.start+dataMap.rownum < len(dataMap.keys) {
			dataMap.start++
		}
		p.renderTopPane()
	} else {
		bottom := p.height - (p.height/2 + 1)
		d := p.lineData[p.curLine-1]
		if l := len(d.sortedKeys); p.curLine < len(p.lineData) {
			if bottom+p.dataIdx >= l {
				p.curLine++
				p.dataIdx = 0
			} else if p.dataIdx < l {
				p.dataIdx++
			}
		} else if p.curLine == len(p.lineData) {
			if bottom+p.dataIdx < l {
				p.dataIdx++
			}
		}
		p.renderBottomPane()
	}
}
