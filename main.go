package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

const (
	ziVersion = "0.0.1"

	// ANSI escape code, 27 in decimal.
	escapeChar = '\x1b'
	// All ANSI escape sequences start with this char.
	escapeSeqBegin    = '['
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

// TermState is a god-object containing the global editor state.
type TermState struct {
	oldTermios *unix.Termios // The Termios struct at application startup, zi reverts back to this on exit
	winSize    *unix.Winsize // The terminal window size, computed once and not adjust based on signals
	mode       EditorMode    // Current editor modality (i.e. Normal/Visual/Command)
	r          *bufio.Reader // Reader from Stdin to get user input
	w          *bufio.Writer // Writer to Stdout to modify view
	welcomed   bool          // true is the welcome message has already been displayed, or should not be displayed
	cursorX    int           // Current 0 index cursor position
	cursorY    int           // Current 0 index cursor position
	bufferRows []string      // All contents of the file, one element per row
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

// clearScreen clears the entire terminal display, but doesn't flush the writer.
func clearScreen(w *bufio.Writer) {
	// "Cursor Position" to top left.
	fmt.Fprintf(w, "%c%cH", escapeChar, escapeSeqBegin)

	// "Erase in Display", Ps == 2 indicates all of the display should be erased.
	fmt.Fprintf(w, "%c%c2J", escapeChar, escapeSeqBegin)
}

func processNormalModePress(ts *TermState, b byte) {
	switch b {
	case ctrlPress('q'):
		clearScreen(ts.w)
		ts.w.Flush()
		ts.exit(nil)
	case 'i':
		ts.mode = insertMode
	case 'h', 'j', 'k', 'l':
		moveCursor(ts, b)
	}
}

// moveCursor adjusts the cursor position based on the command issued.
// Vim-style hjkl movement are the only supported commands.
func moveCursor(ts *TermState, b byte) {
	switch b {
	case 'h':
		if ts.cursorX > 0 {
			ts.cursorX--
		}
	case 'j':
		if ts.cursorY < int(ts.winSize.Row) {
			ts.cursorY++
		}
	case 'k':
		if ts.cursorY > 0 {
			ts.cursorY--
		}
	case 'l':
		if ts.cursorX < int(ts.winSize.Col) {
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
	ts.exit(errors.New("Visual mode is not implemented"))
}

func processCommandModePress(ts *TermState, b byte) {
	ts.exit(errors.New("Command mode is not implemented"))
}

// runReadLoop begins the infinite main program loop, collecting and acting on keypresses.
func (ts *TermState) processKeyPresses() {
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

// writeWelcomeMsg writes a one-time welcome message to the writer.
func (ts *TermState) writeWelcomeMsg() {
	ts.welcomed = true

	var width int

	msg := fmt.Sprintf("zi -- version %v", ziVersion)
	if len(msg) > int(ts.winSize.Col)+1 {
		msg = msg[:ts.winSize.Col+1]
		width = len(msg)
	} else {
		width = (int(ts.winSize.Col) + 1 + len(msg)) / 2
	}
	fmt.Fprintf(ts.w, "%*s", width, msg)
}

func (ts *TermState) drawRows() {
	// TODO - See below, does it make more sense to clear per line?
	clearScreen(ts.w)

	for i := 0; i < int(ts.winSize.Row); i++ {
		switch {
		// Are we drawing text from the edit buffer?
		case i >= len(ts.bufferRows):
			ts.w.WriteByte('~')
			if !ts.welcomed && i == (int(ts.winSize.Row+1)/3) {
				ts.writeWelcomeMsg()
			}
		default:
			// TODO handle truncation better
			chars := len(ts.bufferRows[i])
			if chars > int(ts.winSize.Col) {
				chars = int(ts.winSize.Col)
			}
			ts.w.WriteString(ts.bufferRows[i][:chars])
		}

		// "Erase in Line", erase the line to the right of the cursor.
		// TODO - not sure about this, maybe makes more sense to call clearScreen once.
		// fmt.Fprintf(ts.w, "%c%cK", escapeChar, escapeSeqBegin)

		ts.w.WriteString("\r\n")
	}

	switch ts.mode {
	case normalMode:
		ts.w.WriteString("NORMAL")
	case insertMode:
		ts.w.WriteString("INSERT")
	}
}

// refreshScreen clears the entier screen, draws the buffer content/placeholders/welcome message
// and flushes everything to Stdin.
func (ts *TermState) refreshScreen() {
	// Do a single flush to term to improve perf.
	defer ts.w.Flush()

	// Hide the cursor during updates to avoid flickering.
	fmt.Fprintf(ts.w, "%c%c?25l", escapeChar, escapeSeqBegin)
	// Unhide cursor after redraw.
	defer fmt.Fprintf(ts.w, "%c%c?25h", escapeChar, escapeSeqBegin)

	ts.drawRows()

	// Move cursor to state pos.
	fmt.Fprintf(ts.w, "%c%c%d;%dH", escapeChar,
		escapeSeqBegin, ts.cursorY+1, ts.cursorX+1)
}

// openEditor looks for a filename cmdline arg, if one was provided it is opened and its contents
// are loaded into the TermState.
func (ts *TermState) openEditor() error {
	// TODO use TempFile to allow periodic writes when starting from blank file
	// https://golang.org/pkg/io/ioutil/#TempFile

	if len(os.Args) < 2 {
		return nil
	}
	// content, err := ioutil.ReadFile(filename)
	f, err := os.Open(os.Args[1])
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ts.bufferRows = append(ts.bufferRows, scanner.Text())
	}

	// Don't display welcome when opening a file.
	ts.welcomed = true

	return nil
}

// exit should be called when program exiting/shutdown is initiated.
func (ts *TermState) exit(err error) {
	// Don't leave the terminal in raw mode on exit.
	disableRawMode(int(os.Stdin.Fd()), ts.oldTermios)

	if err != nil {
		fmt.Printf("Error: %w", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func main() {
	oldTermios, err := enableRawMode(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	ws, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ)
	if err != nil || (ws.Row == 0 && ws.Col == 0) {
		disableRawMode(int(os.Stdin.Fd()), oldTermios)
		panic(err)
	}
	// Termios WinSize uses 1-based indexing, this is annoying and I'd rather
	// deal with this in fewer places and assume 0 indexing otherwise.
	ws.Row--
	ws.Col--

	ts := TermState{
		oldTermios: oldTermios,
		winSize:    ws,
		mode:       normalMode,
		r:          bufio.NewReader(os.Stdin),
		w:          bufio.NewWriter(os.Stdout),
		bufferRows: make([]string, 0),
	}

	// Catch any unexpected panics. Normal exits should happen through ts.exit().
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("stacktrace: \n" + string(debug.Stack()))
			ts.exit(fmt.Errorf("Runtime panic: %v", r))
		}
	}()

	err = ts.openEditor()
	if err != nil {
		ts.exit(err)
	}

	for {
		ts.refreshScreen()
		ts.processKeyPresses()
	}
}
