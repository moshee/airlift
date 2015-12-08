package main

import (
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ktkr.us/pkg/airlift/config"
	"ktkr.us/pkg/fmtutil"
	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/out"
)

type context struct {
	Data interface{}
}

func (c *context) Version() string {
	return VERSION
}

func (c *context) Config() *config.Config {
	return config.Get()
}

// header password
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
	sess, ok := isLoggedIn(g)
	if !ok {
		if g.Request.Method == "POST" {
			return 403, nil
		}
		return 303, out.Reroute("/-/login", g.URL.Path)
	}

	if sess != nil {
		sessions.Update(sess.Id)
	}

	return g.Continue()
}

func isLoggedIn(g *gas.Gas) (*auth.Session, bool) {
	conf := config.Get()

	if conf.Password != nil {
		if sess, _ := auth.GetSession(g); sess == nil {
			return nil, false
		} else {
			return sess, true
		}
	}

	return nil, true
}

// return to the URL that sent the reroute
func reroute(g *gas.Gas) (int, gas.Outputter) {
	out.CheckReroute(g)
	var path string
	err := out.Recover(g, &path)
	if err != nil {
		log.Print("reroute error: ", err)
		path = "/-/config"
	}
	return 302, out.Redirect(path)
}

type File struct {
	ID       string
	Name     string
	Uploaded time.Time
	HasThumb bool
	Size     fmtutil.Bytes
}

func (f *File) Ext() string {
	return filepath.Ext(f.Name)
}

func (f *File) Ago() string {
	n := time.Now().Sub(f.Uploaded)
	if n < time.Second {
		return "just now"
	}

	return fmtutil.LongDuration(n)
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
			Size:     fmtutil.Bytes(fi.Size()),
		}
	}

	fileCache.RUnlock()

	return list
}
