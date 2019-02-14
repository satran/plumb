package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"
	"unsafe"
)

func main() {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fd := f.Fd()

	orig := enableRawMode(fd)
	defer disableRawMode(fd, orig)
	fatal := func(err error) {
		disableRawMode(fd, orig)
		log.Fatal(err)
	}
	rows, cols, err := getWindowSize()
	if err != nil {
		fatal(err)
	}
	t := &terminal{
		rows:  rows,
		cols:  cols,
		tty:   bufio.NewReader(f),
		stdin: os.Stdin,
	}
	for {
		if err := t.draw(); err != nil {
			fatal(err)
		}
		if err := t.keypress(); err != nil {
			fatal(err)
		}
	}
}

type terminal struct {
	cx, cy     int
	rows, cols int // rows and cols available in the terminal
	stdin      io.Reader
	tty        *bufio.Reader
	line       int // current line
}

func (t *terminal) draw() error {
	// [y;xH moves the cursor to the appropriate position (x,y)
	//ab += fmt.Sprintf("\x1b[%d;%dH", (t.cy-E.RowOff)+1, (E.Cx-E.ColOff)+1)

	r := bufio.NewReader(t.stdin)
	b := &bytes.Buffer{}
	b.WriteString("\x1b[?25l") // hide cursor
	b.WriteString("\x1b[H")    // move cursor to the top
	b.WriteString("\x1b[K")    // clear line before printing
	x, y := 0, 0
	for {
		by, err := r.ReadByte()
		if err != nil && err != io.EOF {
			return err
		}
		switch by {
		case '\n':
			b.WriteString("\r\n")   // the terminal takes this as a new line and repositions the cursor
			b.WriteString("\x1b[K") // clear line before printing
			x = 0
			y++
		default:
			x++
			b.WriteByte(by)
		}
		if y >= t.rows {
			break
		}
		if x >= t.cols {
			break
		}
		if err == io.EOF {
			break
		}
	}
	b.WriteString("\x1b[?25h") //show cursor
	_, err := io.Copy(os.Stdout, b)
	return err
}

var errExit = errors.New("clean exit")

func (t *terminal) keypress() error {
	r, err := readKey(t.tty)
	if err != nil {
		return err
	}
	switch r {
	case controlKey('q'):
		return errExit
	case ArrowDown, ArrowUp, ArrowLeft, ArrowRight:
		t.moveCursor(r)
	case HomeKey:
		t.cx = 0
	case EndKey:
		t.cx = t.cols - 1
	case PageUp, PageDown:
		times := t.rows
		for i := 0; i < times; i++ {
			if r == PageUp {
				t.moveCursor(ArrowUp)
			} else {
				t.moveCursor(ArrowDown)
			}
		}
	}
	return nil
}

func controlKey(r rune) rune {
	return r & 0x1f
}

const (
	ArrowLeft rune = iota + 10000
	ArrowRight
	ArrowDown
	ArrowUp
	DelKey
	HomeKey
	EndKey
	PageUp
	PageDown
)

func (t *terminal) moveCursor(key rune) {
	switch key {
	case ArrowLeft:

	case ArrowRight:

	case ArrowUp:

	case ArrowDown:

	}
}

func readKey(in *bufio.Reader) (rune, error) {
	r, _, err := in.ReadRune()
	if err != nil {
		return 0, err
	}
	if r != '\x1b' {
		return r, nil
	}

	seq := make([]byte, 2)
	n, err := in.Read(seq)
	if err != nil {
		return -1, err
	}
	if n != 2 {
		return -1, errors.New("could not read sequence")
	}

	if seq[0] == '[' {
		if seq[1] >= '0' && seq[1] <= '9' {
			r, _, err = in.ReadRune()
			if err != nil {
				return '\x1b', err
			}
			if r != '~' {
				return '\x1b', err
			}
			switch seq[1] {
			case '1':
				return HomeKey, nil
			case '3':
				return DelKey, nil
			case '4':
				return EndKey, nil
			case '5':
				return PageUp, nil
			case '6':
				return PageDown, nil
			case '7':
				return HomeKey, nil
			case '8':
				return EndKey, nil
			}
		} else {
			switch seq[1] {
			case 'A':
				return ArrowUp, nil
			case 'B':
				return ArrowDown, nil
			case 'C':
				return ArrowRight, nil
			case 'D':
				return ArrowLeft, nil
			case 'H':
				return HomeKey, nil
			case 'F':
				return EndKey, nil
			}
		}
	} else if seq[0] == 'O' {
		switch seq[1] {
		case 'H':
			return HomeKey, nil
		case 'F':
			return EndKey, nil
		}
	}

	return '\x1b', nil
}

func enableRawMode(fd uintptr) *syscall.Termios {
	origTermios := tcGetAttr(fd)
	var raw syscall.Termios
	raw = *origTermios

	// IXON disables ^s ^q
	// ICRNL disables ^m to return enter
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK |
		syscall.ISTRIP | syscall.IXON

	// disable carriage returns
	raw.Oflag &^= syscall.OPOST
	raw.Cflag |= syscall.CS8

	// ECHO is to ensure characters are not echoed to the prompt
	// ICANON turns of canonical mode
	// ISIG is to ensure SIGINT SIGSTOP is ignored when pressing ^c ^d
	// IEXTEN disables terminal to wait for input after pressing a ctrl key.
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN |
		syscall.ISIG

	raw.Cc[syscall.VMIN+1] = 0
	raw.Cc[syscall.VTIME+1] = 1
	if e := tcSetAttr(fd, &raw); e != nil {
		log.Fatalf("Problem enabling raw mode: %s\n", e)
	}
	return origTermios
}

func disableRawMode(fd uintptr, t *syscall.Termios) {
	if e := tcSetAttr(fd, t); e != nil {
		log.Fatalf("Problem disabling raw mode: %s\n", e)
	}
}

func tcSetAttr(fd uintptr, termios *syscall.Termios) error {
	// TCSETS+1 == TCSETSW, because TCSAFLUSH doesn't exist
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS+1), uintptr(unsafe.Pointer(termios))); err != 0 {
		return err
	}
	return nil
}

func tcGetAttr(fd uintptr) *syscall.Termios {
	var termios = &syscall.Termios{}
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(termios))); err != 0 {
		log.Fatalf("Problem getting terminal attributes: %s\n", err)
	}
	return termios
}

func getWindowSize() (int, int, error) {
	w := struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&w)),
	)
	if err != 0 { // type syscall.Errno
		// This is a hack to get the position. We move the
		// cursor all the way to the bottom right corner and
		// find cursor position.
		io.WriteString(os.Stdout, "\x1b[999C\x1b[999B")
		return getCursorPosition()
	}
	return int(w.Row), int(w.Col), nil
}

func getCursorPosition() (int, int, error) {
	_, err := io.WriteString(os.Stdout, "\x1b[6n")
	if err != nil {
		return 0, 0, err
	}
	var buffer [1]byte
	var buf []byte
	var cc int
	for cc, _ = os.Stdin.Read(buffer[:]); cc == 1; cc, _ = os.Stdin.Read(buffer[:]) {
		if buffer[0] == 'R' {
			break
		}
		buf = append(buf, buffer[0])
	}
	if string(buf[0:2]) != "\x1b[" {
		return 0, 0, errors.New("failed to read rows and cols from tty")
	}
	var rows, cols int
	if n, err := fmt.Sscanf(string(buf[2:]), "%d;%d", rows, cols); n != 2 || err != nil {
		if err != nil {
			return 0, 0, fmt.Errorf("getCursorPosition: fmt.Sscanf() failed: %s\n", err)
		}
		if n != 2 {
			return 0, 0, fmt.Errorf("getCursorPosition: got %d items, wanted 2\n", n)
		}
		return 0, 0, errors.New("unknown error")
	}
	return rows, cols, nil
}
