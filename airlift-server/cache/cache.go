// Package cache implements the data store behind Airlift server.
package cache

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"

	"ktkr.us/pkg/airlift/airlift-server/config"
	"ktkr.us/pkg/airlift/airlift-server/misc"
)

// Cache is an extremely naïve, map-based, fully in-memory key-value store
// configured as a file cache. Only file locations are stored in memory.
// Persistence is achieved through the file system. It is concurrent-access
// safe through locking.
type Cache struct {
	size  int64                  // the total size of the files
	dir   string                 // path of directory where files are stored
	files map[string]os.FileInfo // map[id]filename
	*sync.RWMutex
}

func New(dirPath string) (*Cache, error) {
	c := &Cache{
		0,
		dirPath,
		make(map[string]os.FileInfo),
		new(sync.RWMutex),
	}

	os.MkdirAll(dirPath, 0755)
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, err
	}
	fis, err := dir.Readdir(0)
	if err != nil {
		return nil, err
	}

	for _, fi := range fis {
		c.size += fi.Size()
		name := fi.Name()
		id := strings.Split(name, ".")[0]
		c.files[id] = fi
	}

	return c, nil
}

func (c *Cache) Size() int64 {
	c.RLock()
	defer c.RUnlock()
	return c.size
}

func (c *Cache) filePath(id string) string {
	fi := c.files[id]
	if fi == nil {
		return ""
	}
	return filepath.Join(c.dir, fi.Name())
}

// GetFile returns the path to the file with the given ID in the cache.
func (c *Cache) Get(id string) string {
	c.RLock()
	defer c.RUnlock()
	return c.filePath(id)
}

func (c *Cache) Stat(id string) os.FileInfo {
	c.RLock()
	defer c.RUnlock()
	return c.files[id]
}

// Put copies a file to disk with the given filename and returns its hash.
func (c *Cache) Put(content io.Reader, filename string) (string, error) {
	os.MkdirAll(c.dir, 0700)
	dest := filepath.Join(c.dir, filename)
	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		os.Remove(dest)
		return "", err
	}

	defer destFile.Close()

	sha := sha3.New256()
	w := io.MultiWriter(destFile, sha)
	n, err := io.Copy(w, content)
	if err != nil {
		os.Remove(dest)
		return "", err
	}
	hash := misc.MakeHash(sha.Sum(nil))

	if f, exist := c.files[hash]; exist {
		log.Printf("overwriting existing file: %s (%d -> %d bytes)", f.Name(), f.Size(), n)
		os.Remove(c.filePath(hash))
	}

	destPath := filepath.Join(c.dir, hash+"."+filename)
	if err := os.Rename(dest, destPath); err != nil {
		os.Remove(dest)
		return "", err
	}

	fi, err := os.Stat(destPath)
	if err != nil {
		os.Remove(dest)
		return "", err
	}

	conf := config.Get()
	if conf.MaxSize > 0 {
		c.CutToSize(conf.MaxSize)
	}

	c.Lock()
	c.files[hash] = fi
	c.Unlock()

	return hash, nil
}

func (c *Cache) removeFile(id string) error {
	size := c.files[id].Size()
	if err := os.Remove(c.filePath(id)); err != nil {
		return err
	}
	c.size -= size
	delete(c.files, id)
	return nil
}

// Remove removes a file from the cache.
func (c *Cache) Remove(id string) error {
	c.Lock()
	defer c.Unlock()
	return c.removeFile(id)
}

// RemoveOlderThan removes all files in the cache that were modified before t.
// If an error is encountered while deleting a file, it will not advance any
// further.
func (c *Cache) RemoveOlderThan(t time.Time) error {
	c.Lock()
	defer c.Unlock()
	for id, fi := range c.files {
		if fi.ModTime().Before(t) {
			if err := c.removeFile(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveNewest removes the most recently modified item in the cache. It
// returns the ID of the file that was removed and an error if one was
// encountered.
func (c *Cache) RemoveNewest() (string, error) {
	if len(c.files) == 0 {
		return "", nil
	}
	c.RLock()
	var (
		newest   os.FileInfo
		newestID string
	)
	for id, fi := range c.files {
		if newest != nil && fi.ModTime().After(newest.ModTime()) {
			newest = fi
			newestID = id
		}
	}
	c.RUnlock()

	c.Lock()
	defer c.Unlock()
	return newestID, c.removeFile(newestID)
}

// CutToSize removes the oldest file in the cache until the total size is at
// most n bytes.
func (c *Cache) CutToSize(n int64) error {
	c.Lock()
	defer c.Unlock()
	for c.size > n && len(c.files) > 0 {
		if err := c.removeOldest(); err != nil {
			return err
		}
	}
	return nil
}

// removeOldest removes the oldest file in the cache.
func (c *Cache) removeOldest() error {
	if len(c.files) == 0 {
		return nil
	}
	var (
		oldest   os.FileInfo
		oldestID string
	)
	for id, fi := range c.files {
		if oldest == nil || fi.ModTime().Before(oldest.ModTime()) {
			oldest = fi
			oldestID = id
		}
	}
	return c.removeFile(oldestID)
}

// RemoveAll removes every file in the cache.
func (c *Cache) RemoveAll() error {
	c.Lock()
	defer c.Unlock()
	for id := range c.files {
		if err := c.removeFile(id); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) WatchAges() {
	for {
		conf := config.Get()
		before := time.Now()
		if conf.MaxAge > 0 {
			cutoff := before.Add(-time.Duration(conf.MaxAge) * 24 * time.Hour)
			if err := c.RemoveOlderThan(cutoff); err != nil {
				log.Print(err)
			}
		}
		after := time.Now()
		// execute next on the nearest day
		time.Sleep(before.AddDate(0, 0, 1).Truncate(24 * time.Hour).Sub(after))
	}
}

func (c *Cache) Len() int {
	c.RLock()
	defer c.RUnlock()
	return len(c.files)
}

type byModtime struct {
	ids []string
	fis []os.FileInfo
}

func (s byModtime) Len() int { return len(s.ids) }
func (s byModtime) Less(i, j int) bool {
	a := s.fis[i].ModTime()
	b := s.fis[j].ModTime()
	return a.Before(b)
}
func (s byModtime) Swap(i, j int) {
	s.ids[i], s.ids[j] = s.ids[j], s.ids[i]
	s.fis[i], s.fis[j] = s.fis[j], s.fis[i]
}

func (c *Cache) SortedIDs() []string {
	c.RLock()
	defer c.RUnlock()
	ids := make([]string, 0, len(c.files))
	fis := make([]os.FileInfo, 0, len(c.files))
	for id := range c.files {
		ids = append(ids, id)
		fis = append(fis, c.files[id])
	}
	s := byModtime{ids, fis}

	sort.Sort(s)
	return s.ids
}

func (c *Cache) SetDir(dir string) {
	c.Lock()
	c.dir = dir
	c.Unlock()
}
