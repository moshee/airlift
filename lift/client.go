package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	flag_host     = flag.String("h", "", "Set host to upload to")
	flag_port     = flag.String("p", "", "Set port or interface of remote server to upload to")
	flag_addr     = flag.String("a", "", "Set whole address of server to upload to")
	flag_name     = flag.String("f", "", "Specify a different filename to use. If -z, it names the zip archive")
	flag_stdin    = flag.String("s", "", "Give stdin stream a filename")
	flag_zip      = flag.Bool("z", false, "Upload the input file(s) (and stdin) as a single zip file")
	flag_inclname = flag.Bool("n", false, "Include filename in returned URL")
	flag_nocopy   = flag.Bool("C", false, "Do not copy link to clipboard")
	flag_noprog   = flag.Bool("P", false, "Do not show progress bar")
	dotfilePath   string
)

func init() {
	log.SetFlags(log.Lshortfile)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: lift [options] [ - | files... ]
Options:`)

		flag.PrintDefaults()

		fmt.Fprintln(os.Stderr, `Pass '-' as a filename to include stdin as an upload.
		
Optional parameters specify the connection details to the remote server. -a
sets the entire URL, including scheme (and optional port), and overrides -h and
-p.

If options are specified, they will be saved in the configuration file.
The location of this file is system-dependent:
	$HOME/.airlift on POSIX;
	%LOCALAPPDATA%\airlift\airlift_config on Windows.`)
		os.Exit(1)
	}
	flag.Parse()
}

func main() {
	conf, err := loadConfig()
	if err != nil {
		log.Fatalln(err)
	}

	configured := config(conf)

	if flag.NArg() == 0 {
		if configured {
			os.Exit(0)
		}
		flag.Usage()
	}

	uploads := make([]FileUpload, 0, flag.NArg()+1)

	for _, arg := range flag.Args() {
		if arg == "-" {
			tmp, err := ioutil.TempFile("", "airlift-upload")
			if err != nil {
				log.Fatal("Failed to buffer stdin:", tmp)
			}

			defer os.Remove(tmp.Name())
			defer tmp.Close()

			io.Copy(tmp, os.Stdin)
			tmp.Seek(0, os.SEEK_SET)

			s := FileUpload{"stdin", tmp}
			if *flag_stdin != "" {
				s.Name = *flag_stdin
			}

			uploads = append(uploads, s)
		} else {
			file, err := os.Open(arg)
			if err != nil {
				log.Fatalln(err)
			}
			//name := filepath.Base(file.Name())
			name := file.Name()
			uploads = append(uploads, FileUpload{name, file})
		}
	}

	if *flag_zip {
		uploads = []FileUpload{makeZip(uploads)}
	}

	// -f will simply rename the first file, whatever it is
	if *flag_name != "" {
		uploads[0].Name = *flag_name
	}

	urls := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		u := tryPost(conf, upload)
		if u == "" {
			return
		}
		urls = append(urls, u)
	}

	if !*flag_nocopy {
		str := strings.Join(urls, "\n")
		if err := copyString(str); err != nil {
			if err != errNotCopying {
				fmt.Fprintf(os.Stderr, "(Error copying to clipboard: %v)\n", err)
			}
		} else {
			fmt.Fprintln(os.Stderr, "(Copied to clipboard)")
		}
	}
}

type NotAnError int

func (err NotAnError) Error() string {
	return "this is not an error"
}

const (
	errPassNotFound NotAnError = iota // password not found for host
	errNotCopying                     // not copying anything on this system (no clipboard)
)

func (c *Config) UploadURL() string {
	return c.Scheme + "://" + c.Host + ":" + c.Port + "/upload/file"
}

func loadConfig() (*Config, error) {
	buf, err := ioutil.ReadFile(dotfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			conf := &Config{
				Scheme: "http",
				Port:   "80",
			}
			return conf, writeConfig(conf)
		}
		return nil, err
	}
	conf := new(Config)
	if err := json.Unmarshal(buf, conf); err != nil {
		return nil, fmt.Errorf("Error reading config: %v", err)
	}
	return conf, nil
}

func writeConfig(conf *Config) error {
	dir := filepath.Dir(dotfilePath)
	os.MkdirAll(dir, os.FileMode(0755))
	file, err := os.OpenFile(dotfilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return err
	}
	file.Write(b)
	return nil
}

func config(conf *Config) bool {
	configured := false

	if *flag_host != "" {
		configured = true
		conf.Host = *flag_host
	}
	if *flag_port != "" {
		configured = true
		conf.Port = *flag_port
	}
	if *flag_addr != "" {
		configured = true
		if !strings.Contains(*flag_addr, "://") {
			*flag_addr = "http://" + *flag_addr
		}
		addr, err := url.Parse(*flag_addr)
		if err != nil {
			log.Fatalln("-a:", err)
		}
		conf.Scheme = addr.Scheme
		host, port, err := net.SplitHostPort(addr.Host)
		if err == nil {
			conf.Host, conf.Port = host, port
		} else {
			conf.Host = path.Join(addr.Host, addr.Path)
		}
		if conf.Port == "" {
			conf.Port = "80"
		}
	}
	if conf.Scheme == "" {
		conf.Scheme = "http"
	}

	if configured {
		if err := writeConfig(conf); err != nil {
			log.Fatalln(err)
		}
	}

	return configured
}

type FileUpload struct {
	Name    string
	Content io.ReadCloser
}

// Post file to server. Keep retrying if the password is incorrect,
// otherwise exit with success or other errors.
func tryPost(conf *Config, upload FileUpload) string {
	if conf.Host == "" {
		fmt.Fprintln(os.Stderr, "Host not configured.")
		flag.Usage()
	}

	var alreadyWrong bool

	for {
		resp := postFile(conf, upload)
		var msg Resp
		err := json.NewDecoder(resp.Body).Decode(&msg)
		resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			fmt.Fprintln(os.Stderr, resp.Status)
		}
		if err != nil {
			log.Fatalln("Invalid server response:", err)
		}

		switch resp.StatusCode {
		case http.StatusForbidden:
			if alreadyWrong {
				fmt.Fprintln(os.Stderr, "Sorry, wrong password.")
			} else {
				fmt.Fprintln(os.Stderr, "Server returned error:", msg.Err)
				fmt.Fprintln(os.Stderr, "You'll need a new password. If the request is successful,")
				fmt.Fprintf(os.Stderr, "it will be saved in %s.\n", PasswordStorageMechanism)
				alreadyWrong = true
			}
			fmt.Fprint(os.Stderr, "Password: ")
			pass, err := readPassword()
			if err != nil {
				log.Fatalln(err)
			}
			if err = updatePassword(conf, pass); err != nil {
				log.Fatalln(err)
			}

		case http.StatusCreated:
			u := msg.URL
			if *flag_inclname {
				u = path.Join(u, filepath.Base(upload.Name))
			}
			u = conf.Scheme + "://" + u
			fmt.Println(u)
			return u

		default:
			fmt.Fprintln(os.Stderr, "Server returned error:", msg.Err)
			return ""
		}
	}

}

func postFile(conf *Config, upload FileUpload) *http.Response {
	var (
		sz  int64
		err error
	)

	if seeker, ok := upload.Content.(io.Seeker); ok {
		sz, err = seeker.Seek(0, os.SEEK_END)
		if err != nil {
			log.Fatalln(err)
		}
		seeker.Seek(0, os.SEEK_SET)
	}

	var body io.ReadCloser

	// only show progress if the size is bigger than some arbitrary amount
	// (512KiB) and -P isn't set
	if sz > 512*1024 && !*flag_noprog {
		r := newProgressReader(upload.Content, sz)
		go r.Report()
		body = r
	} else {
		body = upload.Content
	}

	req, err := http.NewRequest("POST", conf.UploadURL(), body)
	if err != nil {
		log.Fatalln(err)
	}

	name := filepath.Base(upload.Name)
	req.Header.Set("X-Airlift-Filename", name)

	// attach the password. Only do so if there's a password stored for the
	// given host.
	pass, err := getPassword(conf)
	switch err {
	case nil:
		req.Header.Set("X-Airlift-Password", pass)
	case errPassNotFound:
		break
	default:
		log.Fatalln(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalln(err)
	}

	return resp
}

type Resp struct {
	URL string
	Err string
}

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
	r.read <- struct {
		n   int
		err error
	}{n, err}
	return
}

func (r *ProgressReader) Close() error {
	r.closed <- struct{}{}
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

	if err != nil {
		log.Fatalln("makeZip:", err)
	}

	done := make(chan struct{}, 1)
	go spinner(done)

	z := zip.NewWriter(tmp)
	now := time.Now()

	for _, upload := range uploads {
		var (
			fh  *zip.FileHeader
			err error
		)
		if ulFile, ok := upload.Content.(*os.File); ok {
			fi, err := ulFile.Stat()
			if err != nil {
				log.Fatalln(err)
			}
			fh, err = zip.FileInfoHeader(fi)
			if err != nil {
				log.Fatalln(err)
			}
		} else {
			fh = &zip.FileHeader{
				Method: zip.Deflate,
			}
			fh.SetModTime(now)
			fh.SetMode(os.FileMode(0644))
		}
		fh.Name = upload.Name
		w, err := z.CreateHeader(fh)
		if err != nil {
			log.Fatalln(err)
		}
		io.Copy(w, upload.Content)
		upload.Content.Close()
	}
	z.Close()

	done <- struct{}{}

	tmp.Seek(0, os.SEEK_SET)
	return FileUpload{"upload.zip", tmp}
}
