package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"code.google.com/p/go.crypto/sha3"

	"github.com/moshee/gas"
)

var (
	logger = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
	appDir string
	files  struct {
		Files map[string]string
		sync.RWMutex
	}
	defaultConfig = Config{
		Host:      "",
		Port:      60606,
		Password:  "",
		Directory: getAppDir("uploads"),
	}
	configLock sync.RWMutex
)

func main() {
	sessDir := getAppDir("sessions")
	os.RemoveAll(sessDir)
	store := &gas.FileStore{Root: sessDir}
	defer store.Destroy()

	gas.UseSessionStore(store)

	conf, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	// make sure the uploads folder is there, and then load all of the file
	// names and IDs into memory
	os.MkdirAll(conf.Directory, os.FileMode(0755))
	files.Files = make(map[string]string)
	list, err := ioutil.ReadDir(conf.Directory)
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range list {
		name := file.Name()
		parts := strings.SplitN(file.Name(), ".", 2)
		files.Files[parts[0]] = name
	}

	gas.New().
		Get("/config", getConfig).
		Post("/config", postConfig).
		Get("/login", getLogin).
		Post("/login", postLogin).
		Get("/logout", getLogout).
		Post("/upload/file", postFile).
		Post("/upload/meta", postFileMeta).
		Get("/{id}", getFile)

	gas.Ignition(&http.Server{
		Addr: ":" + strconv.Itoa(conf.Port),
	})
}

type Config struct {
	Host      string
	Port      int
	Password  string // hash$salt
	Directory string
}

func (c Config) Secrets() (pass, salt []byte, err error) {
	if c.Password == "" {
		return
	}

	parts := strings.SplitN(c.Password, "$", 2)
	pass, err = hex.DecodeString(parts[0])
	if err != nil {
		return
	}
	salt, err = hex.DecodeString(parts[1])
	return
}

func (c Config) Username() string {
	return ""
}

// hash a password, generating a random salt, and return an ASCII representation
func makePass(pass string) string {
	salt := make([]byte, 32)
	rand.Read(salt)
	hash := gas.Hash([]byte(pass), salt)
	return fmt.Sprintf("%x$%x", hash, salt)
}

func getAppDir(more ...string) string {
	u, err := user.Current()
	if err != nil {
		return ""
	}

	return filepath.Join(append([]string{u.HomeDir, ".airlift-server"}, more...)...)
}

func loadConfig() (*Config, error) {
	appDir := getAppDir()
	if err := os.MkdirAll(appDir, os.FileMode(0755)); err != nil {
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
		}
	} else {
		configLock.RLock()
		defer configLock.RUnlock()
		b, err := ioutil.ReadAll(confFile)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(b, &conf)
		if err != nil {
			return nil, err
		}
	}

	return &conf, nil
}

func writeConfig(conf *Config, to string) error {
	b, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return err
	}
	configLock.Lock()
	defer configLock.Unlock()
	file, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return err
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
	if conf.Password != "" {
		if sess, _ := g.Session(); sess == nil {
			return 303, gas.Reroute("/login", "/config")
		}
	}

	return 200, gas.HTML("config", conf, "common")
}

type Error struct {
	Err string
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	oldconf, err := loadConfig()
	if err != nil {
		return 500, gas.JSON(&Error{err.Error()})
	}

	if oldconf.Password != "" {
		got := g.FormValue("oldpass")
		if got == "" {
			return 403, gas.JSON(&Error{"you forgot your password"})
		}
		hash, salt, _ := oldconf.Secrets()
		if !gas.VerifyHash([]byte(got), hash, salt) {
			return 403, gas.JSON(&Error{"incorrect password"})
		}
	}

	conf := Config{
		Host:      g.FormValue("host"),
		Directory: g.FormValue("directory"),
	}

	pass := g.FormValue("password")
	if pass == "" {
		return 400, gas.JSON(&Error{"cannot set empty password"})
	}
	conf.Password = makePass(pass)

	port, err := strconv.Atoi(g.FormValue("port"))
	if err != nil {
		return 400, gas.JSON(&Error{err.Error()})
	}
	conf.Port = port

	path := getAppDir("config")
	err = writeConfig(&conf, path)
	if err != nil {
		return 500, gas.JSON(&Error{err.Error()})
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
		if conf.Password == "" {
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
	files.RLock()
	defer files.RUnlock()

	id := g.Arg("id")
	file, ok := files.Files[id]
	if !ok {
		return 404, g.Error(errors.New("ID not found"))
	}
	path := filepath.Join(conf.Directory, file)
	http.ServeFile(g, g.Request, path)

	return -1, nil
}

type Resp struct {
	URL string
	Err string
}

// optional. May also use SFTP to upload files.
func postFile(g *gas.Gas) (int, gas.Outputter) {
	conf, err := loadConfig()
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	var h = g.Request.Header
	if conf.Password != "" {
		pass := h.Get("X-Airlift-Password")
		if pass == "" {
			return 403, gas.JSON(&Resp{Err: "password required"})
		}
		hash, salt, _ := conf.Secrets()
		if !gas.VerifyHash([]byte(pass), hash, salt) {
			return 403, gas.JSON(&Resp{Err: "incorrect password"})
		}
	}
	filename := h.Get("X-Airlift-Filename")
	if filename == "" {
		return 400, gas.JSON(&Resp{Err: "missing filename header"})
	}
	tmp, err := ioutil.TempFile("", "airlift-upload")
	if err != nil {
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	defer tmp.Close()
	defer g.Body.Close()

	// download file from client to a temp file, taking the sha3 at the same
	// time
	tmpname := tmp.Name()
	sha := sha3.NewKeccak256()
	w := io.MultiWriter(tmp, sha)
	io.Copy(w, g.Body)
	hash := makeHash(sha.Sum(nil))

	// build the ID and URL and move the temp file to the correct location
	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	destName := hash + "." + filename
	dest := filepath.Join(conf.Directory, destName)
	if err = os.Rename(tmpname, dest); err != nil {
		os.Remove(tmpname)
		return 500, gas.JSON(&Resp{Err: err.Error()})
	}

	files.Lock()
	defer files.Unlock()
	files.Files[hash] = destName

	return 201, gas.JSON(&Resp{URL: path.Join(conf.Host, hash)})
}

func postFileMeta(g *gas.Gas) (int, gas.Outputter) {
	return 501, gas.JSON(&Resp{Err: "Not implemented"})
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
