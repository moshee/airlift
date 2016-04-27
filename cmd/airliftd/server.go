package main

import (
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
	"log"
	"math"
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

	_ "net/http/pprof"

	"ktkr.us/pkg/airlift/cache"
	"ktkr.us/pkg/airlift/config"
	"ktkr.us/pkg/airlift/contentdisposition"
	"ktkr.us/pkg/airlift/thumb"
	"ktkr.us/pkg/fmtutil"
	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/out"
	"ktkr.us/pkg/vfs"
	"ktkr.us/pkg/vfs/bindata"
)

//go:generate bindata -skip=*.sw[nop] static templates

var (
	appDir     string // the place where all the stuff is stored
	fileCache  *cache.Cache
	thumbCache *thumb.Cache
	sessions   *auth.FileStore

	VERSION = "devel"
)

const (
	placeholderThumb = "/static/file.svg"
	thumbWidth       = 100
	thumbHeight      = 100

	// https://dev.twitter.com/cards/types/summary-large-image
	// "Images for this Card should be at least 280px in width, and at least
	// 150px in height. Image must be less than 1MB in size."
	twitterThumbWidth  = 700
	twitterThumbHeight = 375

	appDirName = ".airliftd"
)

type Resp struct {
	URL string `json:",omitempty"`
	Err string `json:",omitempty"`
}

func main() {
	var (
		flagPort    = flag.Int("p", -1, "Override port in config")
		flagRsrcDir = flag.String("rsrc", "", "Look for static and template resources in `DIR` (empty = use embedded resources)")
		flagDebug   = flag.Bool("debug", false, "Enable debug/pprof server")
		flagVersion = flag.Bool("v", false, "Show version and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println("airlift server", VERSION)
		return
	} else {
		log.Println("this is airlift server", VERSION)
	}

	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	appDir = filepath.Join(u.HomeDir, appDirName)
	if err := os.MkdirAll(appDir, os.FileMode(0700)); err != nil {
		log.Fatal(err)
	}

	config.Default = config.Config{
		Host:      "",
		Port:      60606,
		HashLen:   4,
		Directory: filepath.Join(appDir, "uploads"),
	}
	if err := config.Init(filepath.Join(appDir, "config")); err != nil {
		log.Fatal(err)
	}
	config.OnSave = func(c *config.Config) {
		fileCache.SetDir(c.Directory)
	}

	gas.Hook(syscall.SIGHUP, func() {
		log.Print("reloading config...")
		if err := config.Reload(); err != nil {
			log.Print(err)
		} else {
			log.Print("reloaded config")
		}
	})
	if *flagDebug {
		go func() {
			log.Fatal(http.ListenAndServe(":6060", nil))
		}()
	}

	if *flagRsrcDir != "" {
		log.Print("using disk filesystem")
		fs, err := vfs.NewNativeFS(*flagRsrcDir)
		if err != nil {
			log.Fatal(err)
		}
		out.TemplateFS(fs)
	} else {
		log.Print("using binary filesystem")
		out.TemplateFS(bindata.Root)
	}

	sessDir := filepath.Join(os.TempDir(), "airliftd-sessions")
	os.RemoveAll(sessDir)
	sessions = &auth.FileStore{Root: sessDir}
	gas.AddDestructor(sessions.Destroy)
	auth.UseSessionStore(sessions)

	conf := config.Get()
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

	r.StaticHandler("/-", *flagRsrcDir).
		Get("/-/login", getLogin).
		Get("/-/logout", getLogout).
		Post("/-/login", postLogin).
		Get("/-/config", checkLogin, getConfig).
		Post("/-/config", checkLogin, postConfig).
		Post("/-/config/size", checkLogin, getSizeLimitPrune).
		Post("/-/config/age", checkLogin, getAgeLimitPrune).
		Get("/-/config/overview", checkLogin, getConfigOverview).
		Post("/upload/web", checkLogin, postFile).
		Post("/upload/file", checkPassword, postFile).
		Post("/oops", checkPassword, oops).
		Get("/-/l", checkPassword, getList).
		Get("/-/history", checkLogin, getHistory).
		Get("/-/history/{page}", checkLogin, getHistoryPage).
		Post("/purge/thumbs", checkLogin, purgeThumbs).
		Post("/purge/all", checkLogin, purgeAll).
		Get("/-/thumb/{id}.jpg", checkLogin, getThumb).
		Get("/-/twitterthumb/{id}.jpg", getTwitterThumb).
		Delete("/{id}", checkPassword, deleteFile).
		Post("/-/delete/{id}", checkLogin, deleteFile).
		Get("/{id}/{filename}", getFile).
		Get("/{id}.{ext}", getFile).
		Get("/{id}", getFile).
		Get("/", getIndex).
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
	return 200, out.HTML("config", &context{data}, "common")
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

	return 200, out.HTML("%overview", &context{data})
}

func postConfig(g *gas.Gas) (int, gas.Outputter) {
	conf := config.Get()
	newconf := config.Config{}

	if err := g.UnmarshalForm(&newconf); err != nil {
		return 400, out.JSON(&Resp{Err: err.Error()})
	}

	pass := g.FormValue("password")
	newpass := g.FormValue("newpass")
	if conf.Password != nil {
		if pass == "" {
			return 403, out.JSON(&Resp{Err: "you forgot your password"})
		}
		if !auth.VerifyHash([]byte(pass), conf.Password, conf.Salt) {
			return 403, out.JSON(&Resp{Err: "incorrect password"})
		}
		if newpass != "" {
			conf.SetPass(newpass)
		}
	} else {
		if newpass == "" {
			return 400, out.JSON(&Resp{Err: "cannot set empty password"})
		} else {
			conf.SetPass(newpass)
			if err := auth.SignIn(g, conf, newpass); err != nil {
				return 400, out.JSON(&Resp{Err: err.Error()})
			}
		}
	}

	newconf.Password = conf.Password
	newconf.Salt = conf.Salt
	newconf.Port = conf.Port

	if newconf.HashLen < 1 {
		newconf.HashLen = 1
	} else if newconf.HashLen > 64 {
		newconf.HashLen = 64
	}

	newconf.Directory = filepath.Clean(newconf.Directory)

	if newconf.TwitterCardEnable && newconf.TwitterHandle == "" {
		return 400, out.JSON(&Resp{Err: "you must provide a Twitter handle to use Twitter Cards"})
	}

	if newconf.TwitterHandle != "" {
		newconf.TwitterHandle = strings.TrimSpace(newconf.TwitterHandle)
		if !strings.HasPrefix(newconf.TwitterHandle, "@") {
			newconf.TwitterHandle = "@" + newconf.TwitterHandle
		}
	}

	if err := config.Set(&newconf); err != nil {
		log.Println(g.Request.Method, "postConfig:", err)
		return 500, out.JSON(&Resp{Err: err.Error()})
	}

	conf = config.Get()

	if conf.MaxSizeEnable {
		_, err := fileCache.CutToSize(conf.Size * 1024 * 1024)
		if err != nil {
			log.Print(err)
		}
	}
	if conf.MaxAgeEnable {
		cutoff := time.Now().Add(-time.Duration(conf.Age) * 24 * time.Hour)
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

	returnPath := g.FormValue("return")
	if returnPath != "" {
		return 303, out.Reroute("/-/login", returnPath)
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
	return 302, out.Redirect("/-/login")
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
		contentdisposition.SetFilename(g, filename)
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
	return 303, out.Redirect("/-/history/1")
}

func getHistoryPage(g *gas.Gas) (int, gas.Outputter) {
	page, err := g.IntArg("page")
	if err != nil || page < 1 {
		return 303, out.Redirect("/-/history/1")
	}

	l := fileCache.Len()

	offset := (page - 1) * itemsPerPage
	if offset > l {
		return 303, out.Redirect("/-/history/1")
	}
	limit := itemsPerPage
	if l < offset+limit {
		limit = l - offset
	}

	conf := config.Get()

	totalPages := int(math.Ceil(float64(l) / float64(itemsPerPage)))
	if totalPages == 0 {
		totalPages = 1
	}

	p := &historyPage{
		List:        getSortedList(offset, limit),
		CurrentPage: page,
		TotalPages:  totalPages,
		AppendExt:   conf.AppendExt,
	}

	for i := range p.List {
		if thumb.DecodeFunc(p.List[i].Name) != nil {
			p.List[i].HasThumb = true
		}
	}

	if page > 1 {
		p.PrevPage = page - 1
	}

	if l > offset+limit {
		p.NextPage = page + 1
	}

	return 200, out.HTML("history", &context{p}, "common")
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
	if _, ok := isLoggedIn(g); ok {
		return 200, out.HTML("index", &context{}, "common")
	}
	return 200, out.HTML("default-index", &context{})
}
