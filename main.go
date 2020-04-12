package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	// "unicode"

	"golang.org/x/sys/unix"
)

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
	mode       EditorMode
	r          *bufio.Reader
	w          *bufio.Writer
}

// enableRawMode puts fd into raw mode and returns the previous state of the terminal.
func enableRawMode(fd int) (*unix.Termios, error) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	oldTermios := *termios

	// Clear bits for functionality we do not want, recall &^ is bitwise clear.
	//
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
	//
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
		runtime.Goexit()
	case 'i':
		ts.mode = insertMode
	}
}

func processInsertModePress(ts *TermState, b byte) {
	switch b {
	case escapeChar:
		ts.mode = normalMode
	}

}

func processVisualModePress(ts *TermState, b byte) {

}

func processCommandModePress(ts *TermState, b byte) {

}

func drawRows(w *bufio.Writer) {
	for i := 0; i < 24; i++ {
		fmt.Fprintf(w, "~\r\n")
	}
	w.Flush()
}

// clearScreen clears the entire terminal display.
func clearScreen(w *bufio.Writer) {
	// "Cursor Position" to top left.
	fmt.Fprintf(w, "%c%cH", escapeChar, escapeSeqBegin)
	w.Flush()

	// "Erase in Display", Ps == 2 indicates all of the display should be erased.
	fmt.Fprintf(w, "%c%c2J", escapeChar, escapeSeqBegin)
	w.Flush()
}

func refreshScreen(w *bufio.Writer) {
	clearScreen(w)
	drawRows(w)

	// Move cursor back to top left.
	fmt.Fprintf(w, "%c%cH", escapeChar, escapeSeqBegin)
	w.Flush()
}

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

	ts := TermState{
		oldTermios: oldTermios,
		mode:       normalMode,
		r:          bufio.NewReader(os.Stdin),
		w:          bufio.NewWriter(os.Stdout),
	}
	defer exit(&ts)

	for {
		refreshScreen(ts.w)
		processKeyPresses(&ts)
	}
}
