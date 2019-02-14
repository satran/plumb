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
	"time"
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
	go keypress()
	if err := draw(); err != nil {
		disableRawMode(fd, orig)
		log.Fatal(err)
	}
	time.Sleep(10 * time.Second)
}

func keypress() {

}

func draw() error {
	// [y;xH moves the cursor to the appropriate position (x,y)
	//ab += fmt.Sprintf("\x1b[%d;%dH", (E.Cy-E.RowOff)+1, (E.Cx-E.ColOff)+1)

	r := bufio.NewReader(os.Stdin)
	rows, cols, err := getWindowSize()
	if err != nil {
		return err
	}
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
		if y >= rows {
			break
		}
		if x >= cols {
			break
		}
		if err == io.EOF {
			break
		}
	}
	b.WriteString("\x1b[?25h") //show cursor
	_, err = io.Copy(os.Stdout, b)
	return err
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
