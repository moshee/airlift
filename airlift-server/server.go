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

	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/out"
)

var (
	appDir        string // the place where all the stuff is stored
	confPath      string // path of config file
	fileList      *FileList
	thumbCache    *FileList
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
	fileList, thumbCache, err = conf.loadFiles()
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

	r.StaticHandler().
		Get("/login", getLogin).
		Get("/logout", getLogout).
		Post("/login", postLogin).
		Get("/config", checkLogin, getConfig).
		Post("/config", postConfig).
		Post("/upload/file", checkPassword, postFile).
		Post("/oops", checkPassword, oops).
		Get("/l", checkPassword, getList).
		Get("/history", checkLogin, getHistory).
		Get("/history/{page}", checkLogin, getHistoryPage).
		Post("/purge/thumbs", checkLogin, purgeThumbs).
		Post("/purge/all", checkLogin, purgeAll).
		Get("/thumb/{id}.jpg", checkLogin, getThumb).
		Delete("/{id}", checkPassword, deleteFile).
		Post("/delete/{id}", checkLogin, deleteFile).
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

func checkLogin(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	// if there's a password set, only allow user into config if they're logged
	// in, otherwise it's probably the first run and they need to enter one
	if conf.Password != nil {
		if sess, _ := auth.GetSession(g); sess == nil {
			return 303, out.Reroute("/login", g.URL.Path)
		}
	}
	return g.Continue()
}

func getConfig(g *gas.Gas) (int, gas.Outputter) {
	data := &struct {
		Conf        *Config
		NumUploads  SpaceConstrainedInt
		UploadsSize Bytes
		ThumbsSize  Bytes
	}{
		<-configChan,
		SpaceConstrainedInt(len(fileList.Files)),
		Bytes(fileList.Size),
		Bytes(thumbCache.Size),
	}

	return 200, out.HTML("config", data, "common")
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	var form struct {
		Host      string `form:"host"`
		Directory string `form:"directory"`
		NewPass   string `form:"newpass"`
		Password  string `form:"password"`
		Port      int    `form:"port"`
		MaxAge    int    `form:"max-age"`
		MaxSize   int64  `form:"max-size"`
	}

	if err := g.UnmarshalForm(&form); err != nil {
		return 400, out.JSON(&Resp{Err: err.Error()})
	}
	conf := <-configChan

	if conf.Password != nil {
		if form.Password == "" {
			return 403, out.JSON(&Resp{Err: "you forgot your password"})
		}
		if !auth.VerifyHash([]byte(form.Password), conf.Password, conf.Salt) {
			return 403, out.JSON(&Resp{Err: "incorrect password"})
		}
		if form.NewPass != "" {
			conf.setPass(form.NewPass)
		}
	} else {
		if form.NewPass == "" {
			return 400, out.JSON(&Resp{Err: "cannot set empty password"})
		} else {
			conf.setPass(form.NewPass)
		}
	}

	conf.Host = form.Host
	conf.Directory = form.Directory
	conf.Port = form.Port
	conf.MaxAge = form.MaxAge
	conf.MaxSize = form.MaxSize

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
		return reroute(g)
	}

	conf, err := loadConfig()
	if err == nil {
		if conf.Password == nil {
			return reroute(g)
		}
	}

	return 200, out.HTML("login", false, "common")
}

func postLogin(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	if err := auth.SignIn(g, conf, g.FormValue("pass")); err != nil {
		return 200, out.HTML("login", true, "common")
	}
	return reroute(g)
}

func reroute(g *gas.Gas) (int, gas.Outputter) {
	out.CheckReroute(g)
	var path string
	err := out.Recover(g, &path)
	if err != nil {
		log.Print("reroute error: ", err)
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
		panic("something weird is happening")
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	return 201, out.JSON(&Resp{URL: path.Join(host, hash)})
}
func deleteFile(g *gas.Gas) (int, gas.Outputter) {
	id := g.Arg("id")
	if id == "" {
		return 400, out.JSON(&Resp{Err: "file ID not specified"})
	}

	fileList.Lock()
	defer fileList.Unlock()
	if err := fileList.remove(id); err != nil {
		log.Println(g.Request.Method, "deleteFile:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	thumbCache.remove(id)

	return 204, nil
}

func oops(g *gas.Gas) (int, gas.Outputter) {
	conf := <-configChan

	pruned, err := fileList.pruneNewest()
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

type File struct {
	ID       string
	Name     string
	Uploaded time.Time
	HasThumb bool
	Size     Bytes
}

func (f *File) Ext() string {
	ext := filepath.Ext(f.Name)
	if ext != "" {
		ext = ext[1:] // remove dot
	}
	return ext
}

const (
	sec   = time.Second
	min   = sec * 60
	hr    = min * 60
	day   = hr * 24
	week  = day * 7
	month = day * 30
	year  = day * 365
)

func p(n time.Duration, s string) string {
	return fmt.Sprintf("%d%s", n, s)
}

func (f *File) Ago() string {
	n := time.Now().Sub(f.Uploaded)
	switch {
	case n < sec:
		return "just now"
	case n < min:
		return p(n/sec, "s")
	case n < hr:
		return p(n/min, "m")
	case n < day:
		return p(n/hr, "h")
	case n < 2*week:
		return p(n/day, "d")
	case n < month:
		return p(n/week, "w")
	case n < year:
		return p(n/month, "mo")
	default:
		return p(n/year, "y")
	}
}

func getSortedList(offset, limit int) []*File {
	ids := fileList.sortedIds()
	if offset > len(ids) {
		offset = 0
	}
	if limit > len(ids)-offset || limit < 0 {
		limit = len(ids)
	}

	fileList.Lock()

	list := make([]*File, limit)
	ids = ids[len(ids)-limit-offset : len(ids)-offset]
	for i, id := range ids {
		fi := fileList.Files[id]
		list[len(list)-i-1] = &File{
			ID:       id,
			Name:     strings.SplitN(fi.Name(), ".", 2)[1],
			Uploaded: fi.ModTime(),
			Size:     Bytes(fi.Size()),
		}
	}

	fileList.Unlock()

	return list
}

func getList(g *gas.Gas) (int, gas.Outputter) {
	limit, err := strconv.Atoi(g.FormValue("limit"))
	if err != nil {
		limit = 10
	}
	list := getSortedList(0, limit)
	return 200, out.JSON(list)
}

const itemsPerPage = 50

type historyPage struct {
	List        []*File
	CurrentPage int
	NextPage    int
	PrevPage    int
	TotalPages  int
}

func getHistory(g *gas.Gas) (int, gas.Outputter) {
	return 303, out.Redirect("/history/0")
}

func getHistoryPage(g *gas.Gas) (int, gas.Outputter) {
	page, err := g.IntArg("page")
	if err != nil || page < 0 {
		return 303, out.Redirect("/history/0")
	}

	l := len(fileList.Files)

	offset := page * itemsPerPage
	if offset > l {
		return 303, out.Redirect("/history/0")
	}
	limit := itemsPerPage
	if l < offset+limit {
		limit = l - offset
	}

	p := &historyPage{
		List:        getSortedList(offset, limit),
		CurrentPage: page,
		TotalPages:  l/itemsPerPage - 1,
	}

	for i := range p.List {
		if thumbnailFunc(p.List[i].Name) != nil {
			p.List[i].HasThumb = true
		}
	}

	if page > 0 {
		p.PrevPage = page - 1
	}

	if l > offset+limit {
		p.NextPage = page + 1
	}

	return 200, out.HTML("history", p, "common")
}

func getThumb(g *gas.Gas) (int, gas.Outputter) {
	t := thumbPath(g.Arg("id"))
	if t == "" {
		return 302, out.Redirect(placeholderThumb)
	}
	http.ServeFile(g, g.Request, filepath.Join(appDir, "thumb-cache", t))
	return g.Stop()
}

func purgeThumbs(g *gas.Gas) (int, gas.Outputter) {
	if err := thumbCache.purge(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	return 204, out.JSON(&Resp{})
}

func purgeAll(g *gas.Gas) (int, gas.Outputter) {
	if err := fileList.purge(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	if err := thumbCache.purge(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	return 204, out.JSON(&Resp{})
}
