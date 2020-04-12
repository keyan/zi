package main

import (
	"bufio"
	// "fmt"
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

type State struct {
	oldTermios *unix.Termios
	mode       EditorMode
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

func readKeyPress(r *bufio.Reader) byte {
	for b, err := r.ReadByte(); ; b, err = r.ReadByte() {
		if err != nil {
			continue
		}
		return b
		// if unicode.IsControl(rune(b)) {
		// 	fmt.Printf("%d\r\n", b)
		// } else {
		// 	fmt.Printf("%v (%c)\r\n", b, b)
		// }
	}
}

// runReadLoop begins the infinite main program loop, collecting and acting on keypresses.
func processKeyPresses(r *bufio.Reader, w *bufio.Writer, s *State) {
	b := readKeyPress(r)

	// Debugging code
	// if unicode.IsControl(rune(b)) {
	// 	fmt.Printf("%d\r\n", b)
	// } else {
	// 	fmt.Printf("%v (%c)\r\n", b, b)
	// }

	switch s.mode {
	case normalMode:
		processNormalModePress(w, b, s)
	case insertMode:
		processInsertModePress(w, b, s)
	case visualMode:
		processVisualModePress(w, b, s)
	case commandMode:
		processCommandModePress(w, b, s)
	}
}

func processNormalModePress(w *bufio.Writer, b byte, s *State) {
	switch b {
	case ctrlPress('q'):
		clearScreen(w)
		runtime.Goexit()
	case 'i':
		s.mode = insertMode
	}
}

func processInsertModePress(w *bufio.Writer, b byte, s *State) {
	switch b {
	case escapeChar:
		s.mode = normalMode
	}

}

func processVisualModePress(w *bufio.Writer, b byte, s *State) {

}

func processCommandModePress(w *bufio.Writer, b byte, s *State) {

}

func drawRows(w *bufio.Writer) {
	for i := 0; i < 24; i++ {
		nn, err := w.Write([]byte("~\r\n"))
		if err != nil || nn != 3 {
			panic(err)
		}
	}
	w.Flush()
}

// clearScreen clears the entire terminal display.
func clearScreen(w *bufio.Writer) {
	// "Cursor Position" to top left.
	nn, err := w.Write([]byte{escapeChar, escapeSeqBegin, 'H'})
	if err != nil || nn != 3 {
		panic(err)
	}
	w.Flush()

	// "Erase in Display", Ps == 2 indicates all of the display should be erased.
	nn, err = w.Write([]byte{escapeChar, escapeSeqBegin, 2, 'J'})
	if err != nil || nn != 4 {
		panic(err)
	}
	w.Flush()
	// Why doesn't this work on exit?
	// fmt.Fprintf(w, "%c%c2J", escapeChar, escapeSeqBegin)
}

func refreshScreen(w *bufio.Writer) {
	clearScreen(w)
	drawRows(w)

	// Move cursor back to top left.
	nn, err := w.Write([]byte{escapeChar, escapeSeqBegin, 'H'})
	if err != nil || nn != 3 {
		panic(err)
	}
	w.Flush()
}

func exit(s *State) {
	// Don't leave the terminal in raw mode on exit.
	disableRawMode(int(os.Stdin.Fd()), s.oldTermios)

	// runtime.Goexit() is used elsewhere, but calling that on the main goroutine
	// means main() never returns. All other defers should come after this.
	os.Exit(0)
}

func main() {
	oldTermios, err := enableRawMode(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	s := State{oldTermios: oldTermios, mode: normalMode}
	defer exit(&s)

	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for {
		refreshScreen(w)
		processKeyPresses(r, w, &s)
	}
}
