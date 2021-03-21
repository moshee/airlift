package main

import (
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
)

var (
	flag_host     = flag.String("h", "", "Set host to upload to")
	flag_port     = flag.String("p", "", "Set port or interface of remote server to upload to")
	flag_addr     = flag.String("a", "", "Set whole address of server to upload to")
	flag_name     = flag.String("f", "", "Specify a different filename to use. If -z, it names the zip archive")
	flag_stdin    = flag.String("s", "", "Give stdin stream a filename")
	flag_remove   = flag.String("r", "", "Instruct the server to delete the file with a given ID")
	flag_zip      = flag.Bool("z", false, "Upload the input file(s) (and stdin) as a single zip file")
	flag_inclname = flag.Bool("n", false, "Include filename in returned URL (overrides -e)")
	flag_inclext  = flag.Bool("e", false, "Append file extension to returned URL")
	flag_nocopy   = flag.Bool("C", false, "Do not copy link to clipboard")
	flag_noprog   = flag.Bool("P", false, "Do not show progress bar")
	flag_oops     = flag.Bool("oops", false, "Delete the last file uploaded")
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
	defer exit(0)

	conf, err := loadConfig()
	if err != nil {
		fatal(err)
	}

	configured := config(conf)

	if flag.NArg() == 0 {
		if configured {
			return
		}

		if *flag_oops {
			oops(conf)
		} else if *flag_remove != "" {
			remove(conf, *flag_remove)
		} else {
			flag.Usage()
		}
		return
	}

	uploads := make([]FileUpload, 0, flag.NArg()+1)

	for _, arg := range flag.Args() {
		if arg == "-" {
			tmp, err := ioutil.TempFile("", "airlift-upload")
			if err != nil {
				fatal("Failed to buffer stdin:", tmp)
			}

			addTemp(tmp.Name())
			defer tmp.Close()

			io.Copy(tmp, os.Stdin)
			tmp.Close()

			// TODO: content sniffing to autogen some name slightly more
			// meaningful than "stdin"?
			s := FileUpload{"stdin", tmp.Name()}
			if *flag_stdin != "" {
				s.Name = *flag_stdin
			}

			uploads = append(uploads, s)
		} else {
			fi, err := os.Stat(arg)
			if err != nil {
				fatal(err)
			}
			if fi.IsDir() {
				fmt.Fprintf(os.Stderr, "warn: skipping directory '%s'\n", arg)
				continue
			}
			uploads = append(uploads, FileUpload{filepath.Base(arg), arg})
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
		u := postFile(conf, upload)
		if u == "" {
			return
		}
		fmt.Println(u)
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

type Resp struct {
	URL string
	Err string
}

func (conf *Config) TryRequest(makeReq func() *http.Request, expectCode int) Resp {
	if conf.Host == "" {
		fmt.Fprintln(os.Stderr, "Host not configured.")
		flag.Usage()
	}

	var alreadyWrong bool

	for {
		req := makeReq()
		if err := conf.PrepareRequest(req); err != nil {
			fatal(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fatal(err)
		}

		if resp.StatusCode != expectCode {
			fmt.Fprintln(os.Stderr, resp.Status)
		}

		var msg Resp

		if resp.StatusCode != http.StatusNoContent {
			err = json.NewDecoder(resp.Body).Decode(&msg)
			resp.Body.Close()
			if err != nil {
				fatal("Invalid server response:", err)
			}
		}

		switch resp.StatusCode {
		case expectCode:
			return msg

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
				fatal(err)
			}
			if err = updatePassword(conf, pass); err != nil {
				fatal(err)
			}

		default:
			fatal("Server returned error:", msg.Err)
		}
	}
}

func requestMaker(req *http.Request) func() *http.Request {
	return func() *http.Request {
		return req
	}
}

func oops(conf *Config) {
	req, err := http.NewRequest("POST", conf.BaseURL("/oops"), nil)
	if err != nil {
		fatal(err)
	}

	resp := conf.TryRequest(requestMaker(req), http.StatusOK)
	u := resp.URL
	if u != "" {
		fmt.Fprintf(os.Stderr, "Deleted upload at %s://%s.\n", conf.Scheme, u)
	}
}

func remove(conf *Config, id string) {
	req, err := http.NewRequest("DELETE", conf.BaseURL("/"+id), nil)
	if err != nil {
		fatal(err)
	}

	conf.TryRequest(requestMaker(req), http.StatusNoContent)
}

type NotAnError int

func (err NotAnError) Error() string {
	return "this is not an error"
}

const (
	errPassNotFound NotAnError = iota // password not found for host
	errNotCopying                     // not copying anything on this system (no clipboard)
)

func (c *Config) BaseURL(add string) string {
	return c.Scheme + "://" + c.Host + ":" + c.Port + add
}

// attach the password. Only do so if there's a password stored for the given
// host.
func (c *Config) PrepareRequest(req *http.Request) error {
	pass, err := getPassword(c)
	switch err {
	case nil:
		req.Header.Set("X-Airlift-Password", pass)
		return nil
	case errPassNotFound:
		return nil
	default:
		return err
	}
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
	os.MkdirAll(dir, 0755)
	file, err := os.OpenFile(dotfilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
			fatal("-a:", err)
		}
		conf.Scheme = addr.Scheme
		host, port, err := net.SplitHostPort(addr.Host)
		if err == nil {
			conf.Host, conf.Port = path.Join(host, addr.Path), port
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
			fatal(err)
		}
	}

	return configured
}

type FileUpload struct {
	Name string // this is the name as it will be presented to the server
	Path string // this is the path of the file on local disk, which could be anything
}

func postFile(conf *Config, upload FileUpload) string {
	msg := conf.TryRequest(func() *http.Request {
		file, err := os.Open(upload.Path)
		if err != nil {
			fatal(err)
		}

		sz, err := file.Seek(0, os.SEEK_END)
		if err != nil {
			fatal(err)
		}
		file.Seek(0, os.SEEK_SET)

		var body io.ReadCloser

		// only show progress if the size is bigger than some arbitrary amount
		// (512KiB) and -P isn't set
		if sz > 512*1024 && !*flag_noprog {
			r := newProgressReader(file, sz)
			//go r.Report()
			body = r
		} else {
			body = file
		}

		req, err := http.NewRequest("POST", conf.BaseURL("/upload/file"), body)
		if err != nil {
			fatal(err)
		}

		req.Header.Set("X-Airlift-Filename", url.QueryEscape(upload.Name))
		return req
	}, http.StatusCreated)

	u := msg.URL
	if *flag_inclname {
		u = path.Join(u, upload.Name)
	} else if *flag_inclext {
		u += filepath.Ext(upload.Name)
	}
	u = conf.Scheme + "://" + u
	return u
}
