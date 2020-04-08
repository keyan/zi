package main

import (
	"bufio"
	"fmt"
	"os"
	"unicode"

	"golang.org/x/sys/unix"
)

const (
	ioctlReadTermios  = unix.TIOCGETA // unix.TCGETS on linux
	ioctlWriteTermios = unix.TIOCSETA // unix.TCSETS on linux
)

// enableRawMode puts fd into raw mode and returns the previous state of the terminal.
func enableRawMode(fd int) (*unix.Termios, error) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	oldTermios := *termios

	// ICRNL disable \r -> \n conversion.
	// IXON disable Ctrl-S and Ctrl-Q.
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	// OPOST disable output processing, so \r doesn't have \n appended.
	termios.Oflag &^= unix.OPOST
	// ECHO don't echo keypresses.
	// ICANON disable canonical mode.
	// ISIG disable Ctrl-C and Ctrl-Za.
	// IEXTEN disable Ctrl-V.
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8
	// Minimum bytes to read before readByte() returns.
	termios.Cc[unix.VMIN] = 1
	// 1/10ths of a second timeout for readByte().
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, termios); err != nil {
		return nil, err
	}

	return &oldTermios, nil
}

func disableRawMode(fd int, oldTermios *unix.Termios) {
	_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, oldTermios)
	return
}

func main() {
	oldTermios, err := enableRawMode(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	// Don't leave the terminal in raw mode on exit.
	defer disableRawMode(int(os.Stdin.Fd()), oldTermios)

	reader := bufio.NewReader(os.Stdin)
	for b, _ := reader.ReadByte(); b != 'q'; b, _ = reader.ReadByte() {
		if unicode.IsControl(rune(b)) {
			fmt.Printf("%d\r\n", b)
		} else {
			fmt.Printf("%v (%c)\r\n", b, b)
		}
	}
}
