package main

import (
	"bufio"
	"bytes"
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
	flag_host   = flag.String("h", "", "Set host to upload to")
	flag_port   = flag.String("p", "", "Set port or interface of remote server to upload to")
	flag_addr   = flag.String("a", "", "Set whole address of server to upload to")
	flag_name   = flag.String("f", "", "Specify a different filename to use. If stdin is used, it names the stdin stream")
	flag_stdin  = flag.Bool("s", false, "Read from stdin")
	flag_nocopy = flag.Bool("C", false, "Do not copy link to clipboard")
	flag_noprog = flag.Bool("P", false, "Do not show progress bar")
	dotfilePath string
)

func init() {
	log.SetFlags(log.Lshortfile)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: lift [options] <filename>
Options:`)

		flag.PrintDefaults()

		fmt.Fprintln(os.Stderr, `Optional parameters specify the connection details to the remote server. -a
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
		if !*flag_stdin {
			flag.Usage()
		}
	}

	urls := make([]string, 0, flag.NArg()+1)

	if *flag_stdin {
		tmp, err := ioutil.TempFile("", "airlift-upload")
		if err != nil {
			log.Fatal("Failed to buffer stdin:", tmp)
		}
		io.Copy(tmp, os.Stdin)
		tmp.Seek(0, os.SEEK_SET)
		s := FileUpload{"stdin", tmp}
		if *flag_name != "" {
			s.Name = *flag_name
			*flag_name = ""
		}
		u := tryPost(conf, s)
		if u == "" {
			return
		}
		urls = append(urls, u)
	}

	for _, arg := range flag.Args() {
		file, err := os.Open(arg)
		if err != nil {
			log.Fatalln(err)
		}
		name := filepath.Base(file.Name())
		u := tryPost(conf, FileUpload{name, file})
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
	file, err := os.Open(dotfilePath)
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
	defer file.Close()
	conf := new(Config)
	b := new(bytes.Buffer)
	io.Copy(b, file)
	if err := json.Unmarshal(b.Bytes(), conf); err != nil {
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
		if err != nil {
			log.Fatalln("error decoding server response:", err)
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
			ret := conf.Scheme + "://" + msg.URL
			fmt.Println(ret)
			return ret

		default:
			fmt.Fprintln(os.Stderr, resp.Status)
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

	var name string
	if *flag_name != "" {
		name = *flag_name
	} else {
		name = upload.Name
	}
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
		return
	}
	defer func() {
		fmt.Fprint(os.Stdout, "\033[J")
	}()
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
	q := float64(r.width-2)*progress + 1
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
	fmt.Fprint(os.Stderr, "\033[J["+string(r.buf)+"]\n\033[1A")
}

// read a password from stdin, disabling console echo
func readPassword() (string, error) {
	/*
		if err := toggleEcho(false); err != nil {
			return "", err
		}
	*/
	toggleEcho(false)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	s := scanner.Text()
	fmt.Fprintln(os.Stderr)
	if err := scanner.Err(); err != nil {
		return "", err
	}

	/*
		if err := toggleEcho(true); err != nil {
			return "", err
		}
	*/
	toggleEcho(true)
	return s, nil
}
