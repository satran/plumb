package main

import (
	"bufio"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	termbox "github.com/nsf/termbox-go"
)

var debug func(format string, v ...interface{})

func main() {
	d := flag.Bool("debug", true, "write debug logs to debug.log")
	flag.Parse()
	if *d {
		debugFile, err := os.OpenFile("debug.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
		if err != nil {
			log.Fatal(err)
		}
		debug = log.New(debugFile, "", log.Lshortfile).Printf
	} else {
		debug = func(format string, v ...interface{}) {}
	}
	termbox.Init()
	defer termbox.Close()

	fatal := func(err error) {
		termbox.Close()
		log.Fatal(err)
	}

	cols, rows := termbox.Size()
	t := &terminal{
		rows:   rows,
		cols:   cols,
		stdin:  &lineReader{lines: make([][]byte, 0, rows)},
		editor: os.Getenv("EDITOR"),
	}
	if t.editor == "" {
		t.editor = "emacs"
	}
	go t.read(os.Stdin)
	for {
		if err := t.keypress(); err != nil {
			if err != errExit {
				fatal(err)
			}
			return
		}
	}
}

type lineReader struct {
	sync.Mutex
	lines [][]byte
}

func (l *lineReader) Write(p []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	if len(l.lines) == 0 {
		l.lines = append(l.lines, []byte{})
	}
	for _, b := range p {
		if b == '\n' {
			l.lines = append(l.lines, []byte{})
			continue
		}
		last := len(l.lines) - 1
		l.lines[last] = append(l.lines[last], b)
	}
	return len(p), nil
}

func (l *lineReader) Line(i int) ([]byte, error) {
	l.Lock()
	defer l.Unlock()
	if i >= len(l.lines) {
		return nil, errors.New("line not found")
	}
	return l.lines[i], nil
}

func (l *lineReader) Rows() int {
	l.Lock()
	defer l.Unlock()
	return len(l.lines)
}

type terminal struct {
	cx, cy     int
	rows, cols int // rows and cols available in the terminal
	stdin      *lineReader
	tty        *bufio.Reader
	selline    int // current line
	topline    int
	editor     string
}

func (t *terminal) read(stdin io.Reader) {
	for {
		n, err := io.Copy(t.stdin, stdin)
		if err != nil {
			panic(err)
		}
		if n == 0 {
			continue
		}
		if err := t.draw(); err != nil {
			panic(err)
		}
	}
}

func (t *terminal) draw() error {
	cols, rows := termbox.Size()
	termbox.HideCursor()
	for y := 0; y < rows; y++ {
		line, err := t.stdin.Line(y + t.topline)
		if err != nil {
			for x := 0; x < cols; x++ {
				termbox.SetCell(x, y, ' ', termbox.ColorDefault, termbox.ColorDefault)
			}
		}
		x := 0
		for _, r := range string(line) {
			if r == '\t' {
				for i := 1; i <= 8; i++ {
					termbox.SetCell(x, y, ' ', termbox.ColorDefault, termbox.ColorDefault)
					x++
				}
				continue
			}
			termbox.SetCell(x, y, r, termbox.ColorDefault, termbox.ColorDefault)
			x++
		}
		for ; x < cols; x++ {
			termbox.SetCell(x, y, ' ', termbox.ColorDefault, termbox.ColorDefault)
		}
	}
	termbox.SetCursor(t.cx, t.cy)
	return termbox.Flush()
}

var errExit = errors.New("clean exit")

func (t *terminal) keypress() error {
	ev := termbox.PollEvent()
	if ev.Type != termbox.EventKey {
		return nil
	}
	switch ev.Key {
	case termbox.KeyArrowUp, termbox.KeyArrowDown:
		t.moveCursor(ev.Key)
	case termbox.KeyPgup, termbox.KeyPgdn:
		times := t.rows
		for i := 0; i < times; i++ {
			if ev.Key == termbox.KeyPgup {
				t.moveCursor(termbox.KeyArrowUp)
			} else {
				t.moveCursor(termbox.KeyArrowDown)
			}
		}
	case termbox.KeyEnter:
		return t.exec()
	case termbox.KeyCtrlQ:
		return errExit
	}
	return t.draw()
}

func (t *terminal) exec() error {
	line, _ := t.stdin.Line(t.selline)
	chunks := strings.Split(string(line), " ")
	for _, name := range chunks {
		name = strings.TrimSpace(name)
		filechunks := strings.Split(name, ":")
		debug("%#v", filechunks)
		if _, err := os.Stat(filechunks[0]); os.IsNotExist(err) {
			continue
		}
		args := []string{}
		if len(filechunks) > 1 {
			args = append(args, "+"+filechunks[1], filechunks[0])
		} else {
			args = append(args, filechunks[0])
		}
		debug("args: %#v", args)

		cmd := exec.Command(t.editor, args...)
		tty, _ := os.OpenFile("/dev/tty", os.O_WRONLY, os.ModePerm)
		defer tty.Close()
		stdout, err := syscall.Dup(int(os.Stdout.Fd()))
		if err != nil {
			return err
		}
		f := os.NewFile(uintptr(stdout), "stdout")
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.Stdin = tty
		cmd.Stdout = f
		cmd.Stderr = f
		err = cmd.Run()
		if err != nil {
			return err
		}

		return termbox.Sync()
	}
	return nil
}

func (t *terminal) moveCursor(key termbox.Key) {
	switch key {
	case termbox.KeyArrowUp:
		if t.selline == 0 {
			return
		}
		if t.topline == 0 {
			t.cy--
			t.selline--
			return
		}
		t.selline--
		if t.cy == 0 {
			t.topline--
		} else {
			t.cy--
		}

	case termbox.KeyArrowDown:
		if t.selline >= t.stdin.Rows()-1 { // last row
			return
		}
		if t.topline >= t.stdin.Rows()-1 {
			return
		}
		t.selline++
		if t.cy >= t.rows-1 {
			t.topline++
		} else {
			t.cy++
		}
	}
}
