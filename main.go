package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	// "unicode"

	"golang.org/x/sys/unix"
)

const ziVersion = "0.0.1"

const (
	// ANSI escape code, 27 in decimal.
	escapeChar = '\x1b'
	// All ANSI escape sequences start with this char.
	escapeSeqBegin = '['
)

const (
	ioctlReadTermios  = unix.TIOCGETA // unix.TCGETS on linux
	ioctlWriteTermios = unix.TIOCSETA // unix.TCSETS on linux
)

type EditorMode int

const (
	_ EditorMode = iota
	normalMode
	insertMode
	visualMode
	commandMode
)

type TermState struct {
	oldTermios *unix.Termios
	winSize    *unix.Winsize
	mode       EditorMode
	r          *bufio.Reader
	w          *bufio.Writer
	cursorX    int
	cursorY    int
}

// enableRawMode puts fd into raw mode and returns the previous state of the terminal.
func enableRawMode(fd int) (*unix.Termios, error) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	oldTermios := *termios

	// Clear bits for functionality we do not want, recall &^ is bitwise clear.
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP
	// ICRNL disables carriage returns (\r) -> newline (\n) conversion.
	// IXON disables Ctrl-S and Ctrl-Q.
	termios.Iflag &^= unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	// OPOST disables output processing, so \r doesn't have \n appended.
	termios.Oflag &^= unix.OPOST
	// ECHO don't echo keypresses.
	// ICANON disables canonical mode, input is read by-byte not by-line.
	// ISIG disables Ctrl-C and Ctrl-Z.
	// IEXTEN disables Ctrl-V.
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8

	// This might not be desired later, but for now, timeout readByte() after 100ms and
	// don't require a min amount of bytes to read before returning.
	// Minimum bytes to read before readByte() returns.
	termios.Cc[unix.VMIN] = 0
	// 100ms timeout for readByte().
	termios.Cc[unix.VTIME] = 1

	// TODO - might need to specify TCSAFLUSH to indicate when the termios change should apply.
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, termios); err != nil {
		return nil, err
	}

	return &oldTermios, nil
}

// disableRawMode resets the terminal to the original state so that any special flags are cleared.
func disableRawMode(fd int, oldTermios *unix.Termios) {
	_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, oldTermios)
	return
}

// ctrlPress returns the byte value of a key if it were pressed with CTRL.
func ctrlPress(char byte) byte {
	// CTRL + <some key> outputs that byte with bits 5-7 cleared.
	return char & 0x1f
}

// readKeyPress keeps reading from r until a byte is read, then returns it.
func readKeyPress(r *bufio.Reader) byte {
	for b, err := r.ReadByte(); ; b, err = r.ReadByte() {
		if err != nil {
			continue
		}
		return b
	}
}

// runReadLoop begins the infinite main program loop, collecting and acting on keypresses.
func processKeyPresses(ts *TermState) {
	b := readKeyPress(ts.r)

	// Debugging code
	// if unicode.IsControl(rune(b)) {
	// 	fmt.Printf("%d\r\n", b)
	// } else {
	// 	fmt.Printf("%v (%c)\r\n", b, b)
	// }

	switch ts.mode {
	case normalMode:
		processNormalModePress(ts, b)
	case insertMode:
		processInsertModePress(ts, b)
	case visualMode:
		processVisualModePress(ts, b)
	case commandMode:
		processCommandModePress(ts, b)
	}
}

func processNormalModePress(ts *TermState, b byte) {
	switch b {
	case ctrlPress('q'):
		clearScreen(ts.w)
		ts.w.Flush()
		runtime.Goexit()
	case 'i':
		ts.mode = insertMode
	case 'h':
		if ts.cursorX > 0 {
			ts.cursorX--
		}
	case 'j':
		if ts.cursorY < int(ts.winSize.Row)-1 {
			ts.cursorY++
		}
	case 'k':
		if ts.cursorY > 0 {
			ts.cursorY--
		}
	case 'l':
		if ts.cursorX < int(ts.winSize.Col)-1 {
			ts.cursorX++
		}
	}
}

func processInsertModePress(ts *TermState, b byte) {
	switch b {
	case escapeChar:
		ts.mode = normalMode
	}

}

func processVisualModePress(ts *TermState, b byte) {
	panic("Visual mode is not implemented")
}

func processCommandModePress(ts *TermState, b byte) {
	panic("Command mode is not implemented")
}

// writeWelcomeMsg writes a one-time welcome message to the writer.
func writeWelcomeMsg(ts *TermState) {
	var width int

	msg := fmt.Sprintf("zi -- version %v", ziVersion)
	if len(msg) > int(ts.winSize.Col) {
		msg = msg[:ts.winSize.Col]
		width = len(msg)
	} else {
		width = (int(ts.winSize.Col) + len(msg)) / 2
	}
	fmt.Fprintf(ts.w, "%*s", width, msg)
}

func drawRows(ts *TermState) {
	for i := 0; i < int(ts.winSize.Row); i++ {
		if i == (int(ts.winSize.Row) / 3) {
			writeWelcomeMsg(ts)
		} else {
			ts.w.WriteByte('~')
		}
		ts.w.WriteString("\r\n")

		// "Erase in Line", erase the line to the right of the cursor.
		// TODO - not sure about this, maybe makes more sense to call clearScreen once.
		fmt.Fprintf(ts.w, "%c%cK", escapeChar, escapeSeqBegin)
	}

	switch ts.mode {
	case normalMode:
		ts.w.WriteString("NORMAL")
	case insertMode:
		ts.w.WriteString("INSERT")
	}
}

// clearScreen clears the entire terminal display, but doesn't flush the writer.
func clearScreen(w *bufio.Writer) {
	// "Cursor Position" to top left.
	fmt.Fprintf(w, "%c%cH", escapeChar, escapeSeqBegin)

	// "Erase in Display", Ps == 2 indicates all of the display should be erased.
	fmt.Fprintf(w, "%c%c2J", escapeChar, escapeSeqBegin)
}

// refreshScreen
func refreshScreen(ts *TermState) {
	// Do a single flush to term to improve perf.
	defer ts.w.Flush()

	// Hide the cursor during updates to avoid flickering.
	fmt.Fprintf(ts.w, "%c%c?25l", escapeChar, escapeSeqBegin)
	// Unhide cursor after redraw.
	defer fmt.Fprintf(ts.w, "%c%c?25h", escapeChar, escapeSeqBegin)

	drawRows(ts)

	// Move cursor to state pos.
	fmt.Fprintf(ts.w, "%c%c%d;%dH", escapeChar,
		escapeSeqBegin, ts.cursorY+1, ts.cursorX+1)
}

// exit should be called when program exiting/shutdown is initiated.
func exit(ts *TermState) {
	// Don't leave the terminal in raw mode on exit.
	disableRawMode(int(os.Stdin.Fd()), ts.oldTermios)

	// runtime.Goexit() is used elsewhere, but calling that on the main goroutine
	// means main() never returns. All other defers should come after this.
	os.Exit(0)
}

func main() {
	oldTermios, err := enableRawMode(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	ws, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ)
	if err != nil || (ws.Row == 0 && ws.Col == 0) {
		panic(err)
	}

	ts := TermState{
		oldTermios: oldTermios,
		winSize:    ws,
		mode:       normalMode,
		r:          bufio.NewReader(os.Stdin),
		w:          bufio.NewWriter(os.Stdout),
	}
	defer exit(&ts)

	for {
		refreshScreen(&ts)
		processKeyPresses(&ts)
	}
}
