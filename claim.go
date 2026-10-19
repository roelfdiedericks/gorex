package main

// Several windows can show a session, each at its own size, and the
// session has one: the last a window gave it. The window in use gives it
// its size again, when it gets the focus, a pane of it starts showing, or
// the user types or clicks in a pane, so that the session follows the
// window the user is in, also once another window that sized it last is
// gone. A window never answers another's size by itself, so that two
// windows in use at once do not take turns forever.

// claimSize gives a pane's session the pane's size again, when the
// session has another.
func (a *App) claimSize(p *Pane) {
	if p.stream == nil || p.closed || p.info.ID == "" || p.info.Exited {
		return
	}
	cols, rows := p.stream.Size()
	if cols <= 0 || rows <= 0 || p.info.Cols == cols && p.info.Rows == rows {
		return
	}
	// The server says the new size with the next list; until then, the
	// pane does not ask again.
	p.info.Cols, p.info.Rows = cols, rows
	s := p.stream
	go s.Resend()
}
