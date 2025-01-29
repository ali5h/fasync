package fasync

import (
	"context"
	"errors"
	"io"
	"slices"
	"syscall"
)

func (b *FFd) GetMsg() []byte {
	if !b.msgvalid {
		b.ScanMsg()
		if !b.msgvalid {
			if b.Refill() {
				b.ScanMsg()
			}
		}
	}
	return b.buf[b.r:b.w]
}

func (b *FFd) ScanMsg() {
	b.msgvalid = b.w > b.r
}

func (b *FFd) Refill() bool {
	if b.r > 0 {
		copy(b.buf, b.buf[b.r:b.w])
		b.w -= b.r
		b.r = 0
	}
	if b.w >= len(b.buf) {
		panic("buf: tried to fill full buffer")
	}

	n, err := b.file.Read(b.buf[b.w:])
	if err == nil || errors.Is(err, syscall.EAGAIN) {
		// good
	} else if errors.Is(err, io.EOF) {
		b.Eof = true
	} else {
		b.Err = err
	}
	b.w += max(n, 0)
	return n > 0
}

func (b *FFd) SkipBytes(n int) {
	b.r += min(n, b.w-b.r)
	b.msgvalid = false
}

func (b *FFd) WriteTo(w io.Writer) (n int64, err error) {
	for b.GetMsg(); b.msgvalid; b.GetMsg() {
		var r int
		r, err = w.Write(b.buf[b.r:b.w])
		n += int64(r)
		b.SkipBytes(r)
		if err != nil {
			break
		}
	}
	return
}

// FFd holds info about one file descriptor
type FFd struct {
	Err error
	Eof bool

	writer   io.Writer
	file     io.Reader
	buf      []byte
	r        int
	w        int
	msgvalid bool
}

func (_fd *FFd) Close() {
	do(func() {
		if closer, ok := _fd.file.(io.Closer); ok {
			closer.Close()
		}
		if idx := slices.Index(Db.Fd, _fd); idx != -1 {
			Db.Fd[idx] = Db.Fd[len(Db.Fd)-1]
			Db.Fd = Db.Fd[:len(Db.Fd)-1]
		}
	})
}

// FDb manages the state
type FDb struct {
	ctx  context.Context
	done context.CancelFunc
	task chan func()
	Fd   []*FFd // queue of FDs that are still readable
}

func do(f func()) {
	done := make(chan bool, 1)
	Db.task <- func() {
		f()
		done <- true
	}
	<-done
}

type StepFn func()

// AddFile adds a file descriptor to FDb's readFd queue.
func AddFile(f io.Reader, writer io.Writer) {
	ff := &FFd{
		file:   f,
		writer: writer,
		buf:    make([]byte, 4096),
	}
	Db.Fd = append(Db.Fd, ff)
}

// fdRead reads one line from the **first** FFd in the queue.
// If a line is read, print it and re-append the FFd to the end (circular).
// If EOF or error occurs, do NOT re-append.
func fdRead() {
	// Pop the first FD from the queue:
	ff := Db.Fd[0]
	Db.Fd = Db.Fd[1:]
	ff.WriteTo(ff.writer)
	if ff.Err == nil && !ff.Eof {
		// Re-append this FD to the end, so we can read again next time:
		Db.Fd = append(Db.Fd, ff)
	}
	if len(Db.Fd) == 0 {
		// No more FDs to read, so we can stop the loop:
		Db.done()
	}
}

var Steps = []StepFn{
	func() {
		if len(Db.Fd) > 0 {
			fdRead()
		}
	},
}

func MainLoop(ctx context.Context) {
	step := make(chan StepFn, len(Steps))
	for _, fn := range Steps {
		step <- fn
	}
	for {
		select {
		case fn := <-step:
			fn()
			step <- fn
		case t := <-Db.task:
			t()
		case <-ctx.Done():
			return
		case <-Db.ctx.Done():
			return
		}
	}
}

var Db *FDb

func init() {
	ctx, done := context.WithCancel(context.Background())
	Db = &FDb{
		Fd:   make([]*FFd, 0),
		task: make(chan func()),
		ctx:  ctx,
		done: done,
	}
}
