package main

import (
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
	"log"
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

	"golang.org/x/image/draw"

	//_ "net/http/pprof"

	"ktkr.us/pkg/airlift/cache"
	"ktkr.us/pkg/airlift/config"
	"ktkr.us/pkg/airlift/thumb"
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
	thumbWidth       = 100
	thumbHeight      = 100

	// https://dev.twitter.com/cards/types/summary-large-image
	// "Images for this Card should be at least 280px in width, and at least
	// 150px in height. Image must be less than 1MB in size."
	twitterThumbWidth  = 280
	twitterThumbHeight = 150
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
		HashLen:   4,
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
	thumbCache, err = thumb.NewCache(thumbDir, thumbEnc, fileCache, draw.BiLinear)
	if err != nil {
		log.Fatalln("thumb cache:", err)
	}

	fileCache.OnRemove = func(id string) {
		if err := thumbCache.Remove(id); err != nil {
			log.Print(err)
		}
	}

	go fileCache.WatchAges(conf)
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
		Post("/config", checkLogin, postConfig).
		Post("/config/size", checkLogin, getSizeLimitPrune).
		Post("/config/age", checkLogin, getAgeLimitPrune).
		Get("/config/overview", checkLogin, getConfigOverview).
		Post("/upload/web", checkLogin, postFile).
		Post("/upload/file", checkPassword, postFile).
		Post("/oops", checkPassword, oops).
		Get("/l", checkPassword, getList).
		Get("/history", checkLogin, getHistory).
		Get("/history/{page}", checkLogin, getHistoryPage).
		Post("/purge/thumbs", checkLogin, purgeThumbs).
		Post("/purge/all", checkLogin, purgeAll).
		Get("/thumb/{id}.jpg", checkLogin, getThumb).
		Get("/twitterthumb/{id}.jpg", getTwitterThumb).
		Delete("/{id}", checkPassword, deleteFile).
		Post("/delete/{id}", checkLogin, deleteFile).
		Get("/{id}/{filename}", getFile).
		Get("/{id}.{ext}", getFile).
		Get("/{id}", getFile).
		Get("/", checkLogin, getIndex).
		Ignition()
}

func getConfig(g *gas.Gas) (int, gas.Outputter) {
	data := &struct {
		Conf        *config.Config
		NumUploads  int
		UploadsSize fmtutil.Bytes
		ThumbsSize  fmtutil.Bytes
	}{
		config.Get(),
		fileCache.Len(),
		fmtutil.Bytes(fileCache.Size()),
		fmtutil.Bytes(thumbCache.Size()),
	}

	return 200, out.HTML("config", data, "common")
}

func getConfigOverview(g *gas.Gas) (int, gas.Outputter) {
	data := &struct {
		NumUploads  int
		UploadsSize fmtutil.Bytes
		ThumbsSize  fmtutil.Bytes
	}{
		fileCache.Len(),
		fmtutil.Bytes(fileCache.Size()),
		fmtutil.Bytes(thumbCache.Size()),
	}

	return 200, out.HTML("overview", data)
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	var form struct {
		Host        string `form:"host"`
		Directory   string `form:"directory"`
		NewPass     string `form:"newpass"`
		Password    string `form:"password"`
		Port        int    `form:"port"`
		HashLen     int    `form:"hash-len"`
		MaxAge      int    `form:"max-age"`
		MaxSize     int64  `form:"max-size"`
		AppendExt   bool   `form:"append-ext"`
		TwitterCard bool   `form:"twitter-card"`
		Handle      string `form:"twitter-handle"`
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
	//conf.HashLen = form.HashLen
	conf.Age = form.MaxAge
	conf.Size = form.MaxSize
	conf.AppendExt = form.AppendExt
	conf.TwitterCardEnable = form.TwitterCard
	conf.TwitterHandle = form.Handle

	if err := config.Set(conf); err != nil {
		log.Println(g.Request.Method, "postConfig:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	if conf.MaxSize() > 0 {
		_, err := fileCache.CutToSize(conf.MaxSize() * 1024 * 1024)
		if err != nil {
			log.Print(err)
		}
	}
	if conf.MaxAge() > 0 {
		cutoff := time.Now().Add(-time.Duration(conf.MaxAge()) * 24 * time.Hour)
		_, err := fileCache.RemoveOlderThan(cutoff)
		if err != nil {
			log.Print(err)
		}
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

	return 200, out.HTML("login", false)
}

func postLogin(g *gas.Gas) (int, gas.Outputter) {
	conf := config.Get()

	if err := auth.SignIn(g, conf, g.FormValue("pass")); err != nil {
		return 200, out.HTML("login", true)
	}
	return reroute(g)
}

func getLogout(g *gas.Gas) (int, gas.Outputter) {
	if err := auth.SignOut(g); err != nil {
		log.Println(g.Request.Method, "getLogout:", err)
		return 500, out.Error(g, err)
	}
	return 302, out.Redirect("/login")
}

func getFile(g *gas.Gas) (int, gas.Outputter) {
	id := g.Arg("id")
	file := fileCache.Get(id)
	if file == "" {
		return 404, out.Error(g, errors.New("ID not found"))
	}

	conf := config.Get()
	if conf.TwitterCardEnable && thumb.FormatSupported(filepath.Ext(file)) {
		uas := g.UserAgents()
		if len(uas) != 0 {
			for _, ua := range uas {
				if ua.Name == "Twitterbot" {
					fi := fileCache.Stat(id)
					host := conf.Host
					if host == "" {
						host = g.Request.Host
					}
					return 200, out.HTML("twitterbot", &struct {
						ID       string
						Name     string
						Uploaded time.Time
						Size     fmtutil.Bytes
						Host     string
						Handle   string
					}{
						id,
						strings.SplitN(fi.Name(), ".", 2)[1],
						fi.ModTime(),
						fmtutil.Bytes(fi.Size()),
						host,
						conf.TwitterHandle,
					})
				}
			}
		}
	}

	if g.Arg("filename") == "" {
		filename := strings.SplitN(filepath.Base(file), ".", 2)[1]
		encoded := strings.Replace(url.QueryEscape(filename), "+", "%20", -1)
		// RFC2616 §2.2 - syntax of quoted strings
		escaped := strings.Replace(filename, `\`, `\\`, -1)
		escaped = strings.Replace(escaped, `"`, `\"`, -1)
		// RFC5987 §3.2.1 - syntax of regular and extended header value encoding
		disposition := fmt.Sprintf(`filename="%s"; filename*=UTF-8''%s`, escaped, encoded)
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

	hash, err := fileCache.Put(g.Body, filename, conf)
	if err != nil {
		log.Println(g.Request.Method, "postFile:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	host := conf.Host
	if host == "" {
		host = g.Request.Host
	}
	if conf.AppendExt {
		hash += filepath.Ext(filename)
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
	AppendExt   bool
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

	conf := config.Get()

	p := &historyPage{
		List:        getSortedList(offset, limit),
		CurrentPage: page,
		TotalPages:  l / itemsPerPage,
		AppendExt:   conf.AppendExt,
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
	t := thumbCache.Get(g.Arg("id"), thumbWidth, thumbHeight)
	if t == "" {
		return 302, out.Redirect(placeholderThumb)
	}
	http.ServeFile(g, g.Request, t)
	return g.Stop()
}

func getTwitterThumb(g *gas.Gas) (int, gas.Outputter) {
	t := thumbCache.Get(g.Arg("id"), twitterThumbWidth, twitterThumbHeight)
	if t == "" {
		return 404, out.Error(g, errors.New("no thumbnail available"))
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

func getSizeLimitPrune(g *gas.Gas) (int, gas.Outputter) {
	var form struct{ N int }
	if err := g.UnmarshalForm(&form); err != nil {
		return 400, out.JSON(&Resp{Err: err.Error()})
	}
	m := fileCache.MaybeCutToSize(int64(form.N) * 1024 * 1024)
	return 200, out.JSON(&struct{ N int }{m})
}

func getAgeLimitPrune(g *gas.Gas) (int, gas.Outputter) {
	var form struct{ N int }
	if err := g.UnmarshalForm(&form); err != nil {
		return 400, out.JSON(&Resp{Err: err.Error()})
	}
	log.Print(time.Now())
	log.Printf("%d days ago\n", form.N)
	t := time.Now().Add(-time.Duration(form.N) * 24 * time.Hour)
	log.Print(t)
	m := fileCache.MaybeRemoveOlderThan(t)
	log.Print(m)
	return 200, out.JSON(&struct{ N int }{m})
}

func getIndex(g *gas.Gas) (int, gas.Outputter) {
	return 200, out.HTML("index", nil, "common")
}
