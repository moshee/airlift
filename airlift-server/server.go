package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/moshee/gas"
	"github.com/moshee/gas/auth"
	"github.com/moshee/gas/out"
)

var (
	appDir        string // the place where all the stuff is stored
	confPath      string // path of config file
	fileList      *FileList
	defaultConfig = Config{
		Host: "",
		Port: 60606,
	}
	configChan = make(chan *Config)
	reloadChan = make(chan struct{})
	errChan    = make(chan error)
	flagPort   = flag.Int("p", 0, "Override port in config")
)

func init() {
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	appDir = filepath.Join(u.HomeDir, ".airlift-server")
	defaultConfig.Directory = filepath.Join(appDir, "uploads")
	confPath = filepath.Join(appDir, "config")
	flag.Parse()

	gas.Hook(syscall.SIGUSR2, func() {
		reloadChan <- struct{}{}
	})
}

func main() {
	sessDir := filepath.Join(appDir, "sessions")
	os.RemoveAll(sessDir)
	store := &auth.FileStore{Root: sessDir}

	gas.AddDestructor(store.Destroy)

	auth.UseSessionStore(store)

	go configServer()

	conf := <-configChan

	var err error
	fileList, err = conf.loadFiles()
	if err != nil {
		log.Fatalln("loading files:", err)
	}

	go fileList.watchAges()

	r := gas.New()

	if gas.Env.TLSPort > 0 {
		r.Use(redirectTLS)
	}

	gas.Env.Port = conf.Port

	if *flagPort > 0 {
		gas.Env.Port = *flagPort
	}

	r.UseMore(out.CheckReroute).
		Get("/login", getLogin).
		Get("/logout", getLogout).
		Post("/login", postLogin).
		Get("/config", getConfig).
		Post("/config", postConfig).
		Post("/upload/file", checkPassword, postFile).
		Post("/oops", checkPassword, oops).
		Delete("/{id}", checkPassword, deleteFile).
		Get("/{id}/{filename}", getFile).
		Get("/{id}.{ext}", getFile).
		Get("/{id}", getFile).
		Ignition()
}

func checkPassword(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	if conf.Password != nil {
		pass := g.Request.Header.Get("X-Airlift-Password")
		if pass == "" {
			return 403, out.JSON(&Resp{Err: "password required"})
		}
		if !auth.VerifyHash([]byte(pass), conf.Password, conf.Salt) {
			return 403, out.JSON(&Resp{Err: "incorrect password"})
		}
	}

	return g.Continue()
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
		return 302, out.Redirect(fmt.Sprintf("https://%s%s%s", host, port, g.URL.Path))
	}
	return g.Continue()
}

func getConfig(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	// if there's a password set, only allow user into config if they're logged
	// in, otherwise it's probably the first run and they need to enter one
	if conf.Password != nil {
		if sess, _ := auth.GetSession(g); sess == nil {
			return 303, out.Reroute("/login", "/config")
		}
	}

	data := &struct {
		Conf        *Config
		NumUploads  SpaceConstrainedInt
		UploadsSize Bytes
	}{
		conf,
		SpaceConstrainedInt(len(fileList.Files)),
		Bytes(fileList.Size),
	}

	return 200, out.HTML("config", data, "common")
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	conf.Host = g.FormValue("host")
	conf.Directory = g.FormValue("directory")

	if conf.Password == nil {
		pass := g.FormValue("newpass")
		if pass == "" {
			return 400, out.JSON(&Resp{Err: "cannot set empty password"})
		} else {
			conf.setPass(pass)
		}
	} else {
		got := g.FormValue("password")
		if got == "" {
			return 403, out.JSON(&Resp{Err: "you forgot your password"})
		}
		if !auth.VerifyHash([]byte(got), conf.Password, conf.Salt) {
			return 403, out.JSON(&Resp{Err: "incorrect password"})
		}
	}

	port, err := strconv.Atoi(g.FormValue("port"))
	if err != nil {
		return 400, out.JSON(&Resp{Err: err.Error()})
	}
	conf.Port = port

	sage := g.FormValue("max-age")
	if len(sage) == 0 {
		conf.MaxAge = 0
	} else {
		age, err := strconv.Atoi(sage)
		if err != nil {
			return 400, out.JSON(&Resp{Err: err.Error()})
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
			return 400, out.JSON(&Resp{Err: err.Error()})
		}
		if size < 0 {
			size = 0
		}
		conf.MaxSize = size
	}

	configChan <- conf
	if err := <-errChan; err != nil {
		log.Println(g.Request.Method, "postConfig:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	return 204, nil
}

func getLogin(g *gas.Gas) (int, gas.Outputter) {
	// already logged in
	if sess, _ := auth.GetSession(g); sess != nil {
		return 302, out.Redirect("/config")
	}

	conf, err := loadConfig()
	if err == nil {
		if conf.Password == nil {
			return 302, out.Redirect("/config")
		}
	}

	return 200, out.HTML("login", false, "common")
}

func postLogin(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan
	var path string
	ok := out.Recover(g, &path) == nil

	if err := auth.SignIn(g, conf); err != nil {
		return 200, out.HTML("login", true, "common")
	}

	if !ok {
		path = "/config"
	}
	return 302, out.Redirect(path)
}

func getLogout(g *gas.Gas) (int, gas.Outputter) {
	if err := auth.SignOut(g); err != nil {
		log.Println(g.Request.Method, "getLogout:", err)
		return 500, out.Error(g, err)
	}
	return 302, out.Redirect("/login")
}

func getFile(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan
	file := fileList.get(g.Arg("id"))
	if file == "" {
		return 404, out.Error(g, errors.New("ID not found"))
	}

	if g.Arg("filename") == "" {
		filename := strings.SplitN(file, ".", 2)[1]
		encoded := strings.Replace(url.QueryEscape(filename), "+", "%20", -1)
		disposition := fmt.Sprintf("filename*=UTF-8''%s; filename=%s", encoded, encoded)
		g.Header().Set("Content-Disposition", disposition)
	}

	threeMonthsFromNow := time.Now().Add(time.Hour * 24 * 30 * 3)
	g.Header().Set("Expires", threeMonthsFromNow.Format(http.TimeFormat))
	g.Header().Set("Cache-Control", "public") // enable browser caching for resources behind TLS

	path := filepath.Join(conf.Directory, file)
	http.ServeFile(g, g.Request, path)

	return -1, nil
}

func postFile(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	filename, err := url.QueryUnescape(g.Request.Header.Get("X-Airlift-Filename"))
	if filename == "" {
		return 400, out.JSON(&Resp{Err: "missing filename header"})
	}
	if err != nil {
		return 400, out.JSON(&Resp{Err: "bad format in filename header: " + err.Error()})
	}
	defer g.Body.Close()

	hash, err := fileList.put(conf, g.Body, filename)
	if err != nil {
		log.Println(g.Request.Method, "postFile:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	return 201, out.JSON(&Resp{URL: path.Join(host, hash)})
}
func deleteFile(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	id := g.Arg("id")
	if id == "" {
		return 400, out.JSON(&Resp{Err: "file ID not specified"})
	}

	fileList.Lock()
	defer fileList.Unlock()
	if err := fileList.remove(conf, id); err != nil {
		log.Println(g.Request.Method, "deleteFile:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	return 204, nil
}

func oops(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	pruned, err := fileList.pruneNewest(conf)
	if err != nil {
		log.Println(g.Request.Method, "oops:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	return 200, out.JSON(&Resp{URL: path.Join(host, pruned)})
}
