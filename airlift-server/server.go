package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"code.google.com/p/go.crypto/sha3"

	"github.com/moshee/gas"
)

var (
	appDir        string
	fileList      *FileList
	defaultConfig = Config{
		Host: "",
		Port: 60606,
	}
	configLock sync.RWMutex
)

func init() {
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	appDir = filepath.Join(u.HomeDir, ".airlift-server")
	defaultConfig.Directory = filepath.Join(appDir, "uploads")
}

func main() {
	sessDir := filepath.Join(appDir, "sessions")
	os.RemoveAll(sessDir)
	store := &gas.FileStore{Root: sessDir}

	gas.AddDestructor(store.Destroy)

	gas.UseSessionStore(store)

	conf, err := loadConfig()
	if err != nil {
		gas.LogFatal("%v", err)
	}

	fileList, err = conf.loadFiles()
	if err != nil {
		gas.LogFatal("loading files: %v", err)
	}

	go fileList.watchAges(conf)

	gas.New().
		Use(redirectTLS).
		Get("/config", getConfig).
		Post("/config", postConfig).
		Get("/login", getLogin).
		Post("/login", postLogin).
		Get("/logout", getLogout).
		Post("/upload/file", postFile).
		Get("/{id}/{filename}", getFile).
		Get("/{id}", getFile)

	gas.Env.Port = conf.Port

	gas.Ignition(nil)
}

func redirectTLS(g *gas.Gas) (int, gas.Outputter) {
	if g.TLS == nil && gas.Env.TLSPort > 0 {
		host := g.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		port := ""
		if gas.Env.TLSPort != 443 {
			port = ":" + strconv.Itoa(gas.Env.TLSPort)
		}
		return 302, gas.Redirect(fmt.Sprintf("https://%s%s%s", host, port, g.URL.Path))
	}
	return 0, nil
}

func (conf *Config) loadFiles() (*FileList, error) {
	files := new(FileList)
	// make sure the uploads folder is there, and then load all of the file
	// names and IDs into memory
	os.MkdirAll(conf.Directory, os.FileMode(0700))
	files.Files = make(map[string]os.FileInfo)
	list, err := ioutil.ReadDir(conf.Directory)
	if err != nil {
		return nil, err
	}
	for _, file := range list {
		parts := strings.SplitN(file.Name(), ".", 2)
		files.Files[parts[0]] = file
		files.Size += file.Size()
	}

	return files, nil
}

type FileList struct {
	Files map[string]os.FileInfo
	Size  int64
	sync.RWMutex
}

func (files *FileList) get(id string) string {
	files.RLock()
	defer files.RUnlock()
	return files.Files[id].Name()
}

// put creates a temp file, downloads a post body to it, moves it to the
// uploads, adds the file to the in-memory list, and returns the generated
// hash.
func (files *FileList) put(conf *Config, content io.Reader, filename string) (string, error) {
	tmp, err := ioutil.TempFile("", "airlift-upload")
	if err != nil {
		return "", err
	}

	defer tmp.Close()

	// download file from client to a temp file, taking the sha3 at the same
	// time
	tmpname := tmp.Name()
	sha := sha3.NewKeccak256()
	w := io.MultiWriter(tmp, sha)
	io.Copy(w, content)
	hash := makeHash(sha.Sum(nil))

	// build the ID and URL and move the temp file to the correct location
	destName := hash + "." + filename
	dest := filepath.Join(conf.Directory, destName)
	if err = os.Rename(tmpname, dest); err != nil {
		os.Remove(tmpname)
		return "", err
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return "", err
	}

	files.Lock()
	defer files.Unlock()
	files.Files[hash] = fi
	files.Size += fi.Size()

	if conf.MaxSize > 0 {
		files.pruneOldest(conf)
	}
	return hash, nil
}

func (files *FileList) pruneOldest(conf *Config) {
	ids := make([]string, 0, len(files.Files))
	for id := range files.Files {
		ids = append(ids, id)
	}

	sort.Sort(byModtime(ids))
	pruned := int64(0)
	n := 0
	for ; files.Size > conf.MaxSize*1024*1024 && len(ids) > 0; ids = ids[1:] {
		id := ids[0]
		f := files.Files[id]
		name := filepath.Join(conf.Directory, f.Name())
		if err := os.Remove(name); err != nil {
			gas.LogWarning("pruning %s: %v", f.Name(), err)
			continue
		}
		delete(files.Files, id)
		files.Size -= f.Size()
		pruned += f.Size()
		n++
	}
	if n > 0 {
		gas.LogNotice("Pruned %d uploads (%.2fMB) to keep under %dMB",
			n, float64(pruned)/(1024*1024), conf.MaxSize)
	}
}

type byModtime []string

func (s byModtime) Len() int { return len(s) }
func (s byModtime) Less(i, j int) bool {
	a := fileList.Files[s[i]].ModTime()
	b := fileList.Files[s[j]].ModTime()
	return a.Before(b)
}
func (s byModtime) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (files *FileList) watchAges(conf *Config) {
	for {
		before := time.Now()
		if conf.MaxAge > 0 {
			cutoff := before.Add(-time.Duration(conf.MaxAge) * 24 * time.Hour)
			files.pruneOld(conf, cutoff)
		}
		after := time.Now()
		// execute next on the nearest day
		time.Sleep(before.AddDate(0, 0, 1).Truncate(24 * time.Hour).Sub(after))
	}
}

func (files *FileList) pruneOld(conf *Config, cutoff time.Time) {
	files.Lock()
	defer files.Unlock()
	n := 0
	for id, fi := range files.Files {
		if fi.ModTime().Before(cutoff) {
			name := filepath.Join(conf.Directory, fi.Name())
			if err := os.Remove(name); err != nil {
				gas.LogWarning("Error pruning %s: %v", fi.Name(), err)
				continue
			}
			n++
			delete(files.Files, id)
		}
	}
	if n > 0 {
		gas.LogNotice("%d upload(s) modified before %s pruned.", n, cutoff.Format("2006-01-02"))
	}
}

type Config struct {
	Host      string
	Port      int
	Password  []byte
	Salt      []byte
	Directory string
	MaxAge    int   // max age of uploads in days
	MaxSize   int64 // max total size of uploads in MB
}

// satisfies gas.User interface
func (c Config) Secrets() (pass, salt []byte, err error) {
	return c.Password, c.Salt, nil
}

func (c Config) Username() string {
	return ""
}

// Update the config with the new password hash, generating a new random salt
func (c *Config) setPass(pass string) {
	c.Salt = make([]byte, 32)
	rand.Read(c.Salt)
	c.Password = gas.Hash([]byte(pass), c.Salt)
}

func loadConfig() (*Config, error) {
	if err := os.MkdirAll(appDir, os.FileMode(0700)); err != nil {
		return nil, err
	}
	var conf Config

	confPath := filepath.Join(appDir, "config")
	confFile, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			conf = defaultConfig
			err = writeConfig(&conf, confPath)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("reading config: %v", err)
		}
	} else {
		configLock.RLock()
		defer configLock.RUnlock()
		b, err := ioutil.ReadAll(confFile)
		if err != nil {
			return nil, fmt.Errorf("reading config: %v", err)
		}
		err = json.Unmarshal(b, &conf)
		if err != nil {
			return nil, fmt.Errorf("decoding config: %v", err)
		}
	}

	return &conf, nil
}

func writeConfig(conf *Config, to string) error {
	b, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return fmt.Errorf("encoding config: %v", err)
	}
	configLock.Lock()
	defer configLock.Unlock()
	file, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("writing config: %v", err)
	}
	defer file.Close()
	file.Write(b)
	return nil
}

func getConfig(g *gas.Gas) (int, gas.Outputter) {
	conf, err := loadConfig()
	if err != nil {
		return 500, g.Error(err)
	}

	// if there's a password set, only allow user into config if they're logged
	// in, otherwise it's probably the first run and they need to enter one
	if conf.Password != nil {
		if sess, _ := g.Session(); sess == nil {
			return 303, gas.Reroute("/login", "/config")
		}
	}

	return 200, gas.HTML("config", conf, "common")
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	oldconf, err := loadConfig()
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	if oldconf.Password != nil {
		got := g.FormValue("oldpass")
		if got == "" {
			return 403, gas.JSON(&Resp{Err: "you forgot your password"})
		}
		if !gas.VerifyHash([]byte(got), oldconf.Password, oldconf.Salt) {
			return 403, gas.JSON(&Resp{Err: "incorrect password"})
		}
	}

	conf := Config{
		Host:      g.FormValue("host"),
		Directory: g.FormValue("directory"),
	}

	pass := g.FormValue("password")
	if pass == "" {
		if oldconf.Password == nil {
			return 400, gas.JSON(&Resp{Err: "cannot set empty password"})
		}
	} else {
		conf.setPass(pass)
	}

	port, err := strconv.Atoi(g.FormValue("port"))
	if err != nil {
		return 400, gas.JSON(&Resp{Err: err.Error()})
	}
	conf.Port = port

	sage := g.FormValue("max-age")
	if len(sage) == 0 {
		conf.MaxAge = 0
	} else {
		age, err := strconv.Atoi(sage)
		if err != nil {
			return 400, gas.JSON(&Resp{Err: err.Error()})
		}
		if age < 0 {
			age = 0
		}
		conf.MaxAge = age
	}

	ssize := g.FormValue("max-size")
	if len(ssize) == 0 {
		conf.MaxSize = 0
	} else {
		size, err := strconv.ParseInt(ssize, 10, 64)
		if err != nil {
			return 400, gas.JSON(&Resp{Err: err.Error()})
		}
		if size < 0 {
			size = 0
		}
		conf.MaxSize = size
	}

	path := filepath.Join(appDir, "config")
	err = writeConfig(conf, path)
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	return 204, nil
}

func getLogin(g *gas.Gas) (int, gas.Outputter) {
	// already logged in
	if sess, _ := g.Session(); sess != nil {
		return 302, gas.Redirect("/config")
	}

	conf, err := loadConfig()
	if err == nil {
		if conf.Password == nil {
			return 302, gas.Redirect("/config")
		}
	}

	return 200, gas.HTML("login", false, "common")
}

func postLogin(g *gas.Gas) (int, gas.Outputter) {
	conf, err := loadConfig()
	if err != nil {
		return 500, g.Error(err)
	}
	var path string
	ok := g.Recover(&path) == nil

	if err := g.SignIn(conf); err != nil {
		return 200, gas.HTML("login", true, "common")
	}

	if !ok {
		path = "/config"
	}
	return 302, gas.Redirect(path)
}

func getLogout(g *gas.Gas) (int, gas.Outputter) {
	if err := g.SignOut(); err != nil {
		return 500, g.Error(err)
	}
	return 302, gas.Redirect("/login")
}

func getFile(g *gas.Gas) (int, gas.Outputter) {
	conf, err := loadConfig()
	if err != nil {
		return 500, g.Error(err)
	}
	file := fileList.get(g.Arg("id"))
	if file == "" {
		return 404, g.Error(errors.New("ID not found"))
	}

	if g.Arg("filename") == "" {
		filename := url.QueryEscape(strings.Split(file, ".")[1])
		g.Header().Set("Content-Disposition", "filename="+filename)
	}

	path := filepath.Join(conf.Directory, file)
	http.ServeFile(g, g.Request, path)

	return -1, nil
}

type Resp struct {
	URL string `json:",omitempty"`
	Err string `json:",omitempty"`
}

func postFile(g *gas.Gas) (int, gas.Outputter) {
	conf, err := loadConfig()
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	var h = g.Request.Header
	if conf.Password != nil {
		pass := h.Get("X-Airlift-Password")
		if pass == "" {
			return 403, gas.JSON(&Resp{Err: "password required"})
		}
		if !gas.VerifyHash([]byte(pass), conf.Password, conf.Salt) {
			return 403, gas.JSON(&Resp{Err: "incorrect password"})
		}
	}
	filename := h.Get("X-Airlift-Filename")
	if filename == "" {
		return 400, gas.JSON(&Resp{Err: "missing filename header"})
	}
	defer g.Body.Close()

	hash, err := fileList.put(conf, g.Body, filename)
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	return 201, gas.JSON(&Resp{URL: path.Join(conf.Host, hash)})
}

func makeHash(hash []byte) string {
	const (
		hashLen = 4
		chars   = "abcdefghijklmnopqrstuvwxyzZYXWVUTSRQPONMLKJIHGFEDCBA1234567890"
	)

	s := make([]byte, hashLen)

	for i, b := range hash {
		s[i%hashLen] ^= b
	}
	for i := range s {
		s[i] = chars[int(s[i])%len(chars)]
	}

	return string(s)
}
