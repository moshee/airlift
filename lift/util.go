package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"time"
)

func newProgressReader(r io.ReadCloser, total int64) *ProgressReader {
	p := &ProgressReader{
		ReadCloser: r,
		total:      total,
		width:      getTermWidth(),
		read: make(chan struct {
			n   int
			err error
		}, 10),
		closed: make(chan struct{}, 1),
	}
	if p.width >= 3 {
		p.buf = make([]rune, p.width-2)
	}
	return p
}

type ProgressReader struct {
	io.ReadCloser
	total   int64
	current int64
	width   int
	buf     []rune
	read    chan struct {
		n   int
		err error
	}
	closed chan struct{}
}

var barChars = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

func (r *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	r.current += int64(n)
	r.output()
	/*
		r.read <- struct {
			n   int
			err error
		}{n, err}
	*/
	return
}

func (r *ProgressReader) Close() error {
	//r.closed <- struct{}{}
	fmt.Fprintln(os.Stderr)
	return r.ReadCloser.Close()
}

func (r *ProgressReader) Report() {
	if r.buf == nil {
		<-r.closed
		return
	}
	defer termClearLine()
	t := time.NewTicker(33 * time.Millisecond)
	for {
		select {
		case <-r.closed:
			r.current = r.total
			r.output()
			return
		case read := <-r.read:
			if read.err != nil {
				return
			}
			r.current += int64(read.n)
		case <-t.C:
			r.output()
		}
	}
}

func (r *ProgressReader) output() {
	last := barChars[len(barChars)-1]
	progress := float64(r.current) / float64(r.total)
	q := float64(r.width-1) * progress
	x := int(q)
	frac := barChars[int((q-float64(x))*float64(len(barChars)))]

	ch := last
	for i := 0; i < len(r.buf); i++ {
		if i == x {
			r.buf[i] = frac
			ch = ' '
		} else {
			r.buf[i] = ch
		}
	}
	//termClearLine()
	os.Stderr.WriteString("[" + string(r.buf) + "]")
	termReturn0()
}

// read a password from stdin, disabling console echo
func readPassword() (string, error) {
	toggleEcho(false)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	s := scanner.Text()
	fmt.Fprintln(os.Stderr)
	if err := scanner.Err(); err != nil {
		return "", err
	}

	toggleEcho(true)
	return s, nil
}

func spinner(done chan struct{}) {
	chars := []rune("▖▙▚▜▝▘▛▞▟▗")
	t := time.NewTicker(33 * time.Millisecond)
	for i := 0; ; i = (i + 1) % len(chars) {
		select {
		case <-done:
			termClearLine()
			return
		case <-t.C:
			fmt.Fprintf(os.Stderr, " %c", chars[i])
			termReturn0()
		}
	}
}

// Write a zip file with the contents of each FileUpload and return a new
// FileUpload containing the zip file. All files will be placed in the root of
// the zip archive (there will be no directories).
func makeZip(uploads []FileUpload) FileUpload {
	if len(uploads) == 0 {
		log.Fatalln("makeZip: no uploads to operate on")
	}
	tmp, err := ioutil.TempFile("", "airlift-upload")
	defer tmp.Close()

	if err != nil {
		log.Fatalln("makeZip:", err)
	}

	done := make(chan struct{}, 1)
	go spinner(done)

	z := zip.NewWriter(tmp)
	//now := time.Now()

	for _, upload := range uploads {
		var (
			fh  *zip.FileHeader
			err error
		)
		ulFile, err := os.Open(upload.Path)
		if err != nil {
			log.Fatal(err)
		}
		fi, err := ulFile.Stat()
		if err != nil {
			log.Fatalln(err)
		}
		fh, err = zip.FileInfoHeader(fi)
		if err != nil {
			log.Fatalln(err)
		}
		fh.Name = upload.Name
		w, err := z.CreateHeader(fh)
		if err != nil {
			log.Fatalln(err)
		}
		io.Copy(w, ulFile)
		ulFile.Close()
	}
	z.Close()

	done <- struct{}{}

	return FileUpload{"upload.zip", tmp.Name()}
}
