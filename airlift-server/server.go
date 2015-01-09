package main

import (
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
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

	//_ "net/http/pprof"

	"ktkr.us/pkg/airlift/airlift-server/cache"
	"ktkr.us/pkg/airlift/airlift-server/config"
	"ktkr.us/pkg/airlift/airlift-server/thumb"
	"ktkr.us/pkg/fmtutil"
	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/out"
)

var (
	appDir     string // the place where all the stuff is stored
	fileCache  *cache.Cache
	thumbCache *thumb.Cache
	flagPort   = flag.Int("p", -1, "Override port in config")
)

const (
	placeholderThumb = "/static/file.svg"
)

type Resp struct {
	URL string `json:",omitempty"`
	Err string `json:",omitempty"`
}

func init() {
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	appDir = filepath.Join(u.HomeDir, ".airlift-server")
	if err := os.MkdirAll(appDir, os.FileMode(0700)); err != nil {
		log.Fatal(err)
	}
	config.Default = config.Config{
		Host:      "",
		Port:      60606,
		Directory: filepath.Join(appDir, "uploads"),
	}
	confPath := filepath.Join(appDir, "config")
	if err := config.Init(confPath); err != nil {
		log.Fatal(err)
	}
	config.OnSave = func(c *config.Config) {
		fileCache.SetDir(c.Directory)
	}

	flag.Parse()

	gas.Hook(syscall.SIGUSR2, func() {
		if err := config.Reload(); err != nil {
			log.Print(err)
		}
	})
}

func main() {
	/*
		go func() {
			log.Fatal(http.ListenAndServe(":6060", nil))
		}()
	*/
	sessDir := filepath.Join(appDir, "sessions")
	os.RemoveAll(sessDir)
	store := &auth.FileStore{Root: sessDir}

	gas.AddDestructor(store.Destroy)

	auth.UseSessionStore(store)

	go config.Serve()

	conf := config.Get()
	var err error
	fileCache, err = cache.New(conf.Directory)
	if err != nil {
		log.Fatalln("file list:", err)
	}
	thumbDir := filepath.Join(appDir, "thumb-cache")
	thumbEnc := thumb.JPEGEncoder{&jpeg.Options{Quality: 88}}
	thumbCache, err = thumb.NewCache(thumbDir, thumbEnc, fileCache)
	if err != nil {
		log.Fatalln("thumb cache:", err)
	}

	go fileCache.WatchAges()
	go thumbCache.Serve()

	r := gas.New()

	if gas.Env.TLSPort > 0 {
		r.Use(redirectTLS)
	}

	gas.Env.Port = conf.Port

	if *flagPort > 0 {
		gas.Env.Port = *flagPort
	}

	for _, code := range []int{400, 404, 500} {
		func(code int) {
			r.Get("/"+strconv.Itoa(code), func(g *gas.Gas) (int, gas.Outputter) {
				return code, out.Error(g, errors.New("Blow out the cartridge and try again."))
			})
		}(code)
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
	conf := config.Get()

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
	conf := config.Get()

	// if there's a password set, only allow user into config if they're logged
	// in, otherwise it's probably the first run and they need to enter one
	if conf.Password != nil {
		if sess, _ := auth.GetSession(g); sess == nil {
			return 303, out.Reroute("/login", g.URL.Path)
		}
	}
	return g.Continue()
}

type Bytes uint64

func (b Bytes) String() string {
	return fmtutil.SI(b).String() + "B"
}

func getConfig(g *gas.Gas) (int, gas.Outputter) {
	data := &struct {
		Conf        *config.Config
		NumUploads  int
		UploadsSize Bytes
		ThumbsSize  Bytes
	}{
		config.Get(),
		fileCache.Len(),
		Bytes(fileCache.Size()),
		Bytes(thumbCache.Size()),
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
	conf := config.Get()

	if conf.Password != nil {
		if form.Password == "" {
			return 403, out.JSON(&Resp{Err: "you forgot your password"})
		}
		if !auth.VerifyHash([]byte(form.Password), conf.Password, conf.Salt) {
			return 403, out.JSON(&Resp{Err: "incorrect password"})
		}
		if form.NewPass != "" {
			conf.SetPass(form.NewPass)
		}
	} else {
		if form.NewPass == "" {
			return 400, out.JSON(&Resp{Err: "cannot set empty password"})
		} else {
			conf.SetPass(form.NewPass)
		}
	}

	conf.Host = form.Host
	conf.Directory = form.Directory
	conf.Port = form.Port
	conf.MaxAge = form.MaxAge
	conf.MaxSize = form.MaxSize

	if err := config.Set(conf); err != nil {
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

	conf := config.Get()
	if conf.Password == nil {
		return reroute(g)
	}

	return 200, out.HTML("login", false, "common")
}

func postLogin(g *gas.Gas) (int, gas.Outputter) {
	conf := config.Get()

	if err := auth.SignIn(g, conf, g.FormValue("pass")); err != nil {
		return 200, out.HTML("login", true, "common")
	}
	return reroute(g)
}

// return to the URL that sent the reroute
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
	file := fileCache.Get(g.Arg("id"))
	if file == "" {
		return 404, out.Error(g, errors.New("ID not found"))
	}

	if g.Arg("filename") == "" {
		filename := strings.SplitN(filepath.Base(file), ".", 2)[1]
		encoded := strings.Replace(url.QueryEscape(filename), "+", "%20", -1)
		disposition := fmt.Sprintf("filename*=UTF-8''%s; filename=%s", encoded, encoded)
		g.Header().Set("Content-Disposition", disposition)
	}

	threeMonthsFromNow := time.Now().Add(time.Hour * 24 * 30 * 3)
	g.Header().Set("Expires", threeMonthsFromNow.Format(http.TimeFormat))
	// enable browser caching for resources behind TLS
	g.Header().Set("Cache-Control", "public")

	http.ServeFile(g, g.Request, file)

	return -1, nil
}

func postFile(g *gas.Gas) (int, gas.Outputter) {
	conf := config.Get()

	filename, err := url.QueryUnescape(g.Request.Header.Get("X-Airlift-Filename"))
	if filename == "" {
		return 400, out.JSON(&Resp{Err: "missing filename header"})
	}
	if err != nil {
		return 400, out.JSON(&Resp{Err: "bad format in filename header: " + err.Error()})
	}
	defer g.Body.Close()

	hash, err := fileCache.Put(g.Body, filename)
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
	id := g.Arg("id")
	if id == "" {
		return 400, out.JSON(&Resp{Err: "file ID not specified"})
	}

	if err := fileCache.Remove(id); err != nil {
		log.Println(g.Request.Method, "deleteFile:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	thumbCache.Remove(id)

	return 204, nil
}

func oops(g *gas.Gas) (int, gas.Outputter) {
	conf := config.Get()

	pruned, err := fileCache.RemoveNewest()
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
	ids := fileCache.SortedIDs()
	if offset > len(ids) {
		offset = 0
	}
	if limit > len(ids)-offset || limit < 0 {
		limit = len(ids)
	}

	fileCache.RLock()

	list := make([]*File, limit)
	ids = ids[len(ids)-limit-offset : len(ids)-offset]
	for i, id := range ids {
		fi := fileCache.Stat(id)
		list[len(list)-i-1] = &File{
			ID:       id,
			Name:     strings.SplitN(fi.Name(), ".", 2)[1],
			Uploaded: fi.ModTime(),
			Size:     Bytes(fi.Size()),
		}
	}

	fileCache.RUnlock()

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

	l := fileCache.Len()

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
		TotalPages:  l / itemsPerPage,
	}

	for i := range p.List {
		if thumb.DecodeFunc(p.List[i].Name) != nil {
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
	t := thumbCache.Get(g.Arg("id"))
	if t == "" {
		return 302, out.Redirect(placeholderThumb)
	}
	http.ServeFile(g, g.Request, t)
	return g.Stop()
}

func purgeThumbs(g *gas.Gas) (int, gas.Outputter) {
	if err := thumbCache.Purge(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	return 204, out.JSON(&Resp{})
}

func purgeAll(g *gas.Gas) (int, gas.Outputter) {
	if err := fileCache.RemoveAll(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	if err := thumbCache.Purge(); err != nil {
		return 500, out.JSON(&Resp{Err: err.Error()})
	}
	return 204, out.JSON(&Resp{})
}
