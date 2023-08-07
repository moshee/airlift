// Package cache implements the data store behind Airlift server.
package cache

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

const SHASize = 64

// Config represents the configurable behavior that Cache needs to operate.
type Config interface {
	MaxAge() int
	MaxSize() int64
	MaxCount() int
	Refresh()
	ProcessHash(buf []byte) string
}

// Cache is an extremely naïve, map-based, fully in-memory key-value store
// configured as a file cache. Only file locations are stored in memory.
// Persistence is achieved through the file system. It is concurrent-access
// safe through locking.
type Cache struct {
	// OnRemove is an arbitrary callback that is called whenever a file is
	// successfully removed.
	OnRemove func(id string)

	*sync.RWMutex
	size  int64                  // the total size of the files
	dir   string                 // path of directory where files are stored
	files map[string]os.FileInfo // map[id]filename
}

// New initializes and returns a new cache object rooted in dirPath.
func New(dirPath string) (*Cache, error) {
	c := &Cache{
		nil,
		new(sync.RWMutex),
		0,
		dirPath,
		make(map[string]os.FileInfo),
	}

	os.MkdirAll(dirPath, 0755)
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fis, err := dir.Readdir(0)
	if err != nil {
		return nil, err
	}

	for _, fi := range fis {
		c.size += fi.Size()
		name := fi.Name()
		id := strings.Split(name, ".")[0]
		if id == "" {
			log.Println("hidden file ", name)
			continue
		}
		if id == "dummy" {
			id = strings.Join(strings.Split(name, ".")[1:], ".")
		}
		c.files[id] = fi
	}

	return c, nil
}

// Size returns the total number of bytes taken up by files in the cache.
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

// Stat returns the os.FileInfo associated with a file.
func (c *Cache) Stat(id string) os.FileInfo {
	c.RLock()
	defer c.RUnlock()
	return c.files[id]
}

// Put copies a file to disk with the given filename and returns its hash.
func (c *Cache) Put(content io.Reader, filename string, conf Config) (string, error) {
	os.MkdirAll(c.dir, 0700)
	dest := filepath.Join(c.dir, filename)
	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		os.Remove(dest)
		return "", err
	}

	defer destFile.Close()

	sha := sha3.NewShake256()
	w := io.MultiWriter(destFile, sha)
	_, err = io.Copy(w, content)
	if err != nil {
		os.Remove(dest)
		return "", err
	}

	var (
		buf  = make([]byte, SHASize)
		hash string
	)

	for {
		sha.Read(buf)
		hash = conf.ProcessHash(buf)

		if _, exist := c.files[hash]; exist || hash == "dummy" {
			log.Printf("cache: collision detected with ID '%s' - regenerating", hash)
		} else {
			break
		}
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

	//conf := config.Get()
	if conf.MaxSize() > 0 {
		_, err = c.CutToSize(conf.MaxSize() * 1024 * 1024)
		if err != nil {
			os.Remove(dest)
			return "", err
		}
	}

	if conf.MaxCount() > 0 {
		_, err = c.CutToCount(conf.MaxCount() - 1)
		if err != nil {
			os.Remove(dest)
			return "", err
		}
	}

	c.Lock()
	c.files[hash] = fi
	c.size += fi.Size()
	c.Unlock()

	return hash, nil
}

// PutNoHsh copies a file to disk with the given filename, preserving the name
func (c *Cache) PutNoHash(content io.Reader, filename string, conf Config) error {
	if _, exist := c.files[filename]; exist {
		return fmt.Errorf("A file with the same name already exists")
	}

	os.MkdirAll(c.dir, 0700)
	dest := filepath.Join(c.dir, fmt.Sprintf("dummy.%s", filename))
	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		os.Remove(dest)
		return err
	}

	defer destFile.Close()

	w := io.MultiWriter(destFile)
	_, err = io.Copy(w, content)
	if err != nil {
		os.Remove(dest)
		return err
	}

	fi, err := os.Stat(dest)
	if err != nil {
		os.Remove(dest)
		return err
	}

	if conf.MaxSize() > 0 {
		_, err = c.CutToSize(conf.MaxSize() * 1024 * 1024)
		if err != nil {
			os.Remove(dest)
			return err
		}
	}

	if conf.MaxCount() > 0 {
		_, err = c.CutToCount(conf.MaxCount() - 1)
		if err != nil {
			os.Remove(dest)
			return err
		}
	}

	c.Lock()
	c.files[filename] = fi
	c.size += fi.Size()
	c.Unlock()

	return nil
}

func (c *Cache) removeFile(id string) error {
	size := c.files[id].Size()
	if err := os.Remove(c.filePath(id)); err != nil {
		return err
	}
	c.size -= size
	delete(c.files, id)
	if c.OnRemove != nil {
		c.OnRemove(id)
	}
	return nil
}

// Remove removes a file from the cache.
func (c *Cache) Remove(id string) error {
	c.Lock()
	defer c.Unlock()
	return c.removeFile(id)
}

// RemoveOlderThan removes all files in the cache that were modified before t,
// returning the IDs of the deleted files.  If an error is encountered while
// deleting a file, it will not advance any further.
func (c *Cache) RemoveOlderThan(t time.Time) ([]string, error) {
	c.Lock()
	defer c.Unlock()
	ids := []string{}
	for id, fi := range c.files {
		if fi.ModTime().Before(t) {
			if err := c.removeFile(id); err != nil {
				return nil, err
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// MaybeRemoveOlderThan returns the number of files that would be deleted if
// RemoveOlderThan were to be called.
func (c *Cache) MaybeRemoveOlderThan(t time.Time) int {
	c.RLock()
	defer c.RUnlock()
	ids := c.SortedIDs()
	for i, id := range ids {
		fi := c.files[id]
		if fi != nil && !fi.ModTime().Before(t) {
			return i
		}
	}
	return 0
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
// most n bytes, returning the IDs of the deleted files. Nothing will happen if
// n == 0. To remove all files, use RemoveAll.
func (c *Cache) CutToSize(n int64) ([]string, error) {
	if n == 0 {
		return nil, nil
	}
	c.Lock()
	defer c.Unlock()
	ids := []string{}
	for c.size > n && len(c.files) > 0 {
		id, err := c.removeOldest()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// MaybeCutToSize returns how many files would be pruned if CutToSize would
// have been called.
func (c *Cache) MaybeCutToSize(n int64) (m int) {
	c.RLock()
	defer c.RUnlock()
	target := c.size - n
	s := int64(0)
	ids := c.SortedIDs()
	for i := 0; i < len(ids) && s <= target; i++ {
		m++
		s += c.files[ids[i]].Size()
	}
	return
}

// CutCoCount removes the oldest file until the number of files in the cache is
// less than or equal to n.
func (c *Cache) CutToCount(n int) (m int, err error) {
	c.Lock()
	defer c.Unlock()

	for len(c.files) > n {
		_, err = c.removeOldest()
		if err != nil {
			return
		}
		m++
	}

	return
}

// removeOldest removes the oldest file in the cache.
func (c *Cache) removeOldest() (string, error) {
	if len(c.files) == 0 {
		return "", nil
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
	return oldestID, c.removeFile(oldestID)
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

// WatchAges starts a blocking server that constantly watches for files to
// become stale, and then deletes them. It should be run in its own goroutine.
func (c *Cache) WatchAges(conf Config) {
	for {
		//conf := config.Get()
		conf.Refresh()
		before := time.Now()
		if conf.MaxAge() > 0 {
			cutoff := before.Add(-time.Duration(conf.MaxAge()) * 24 * time.Hour)
			if _, err := c.RemoveOlderThan(cutoff); err != nil {
				log.Print(err)
			}
		}
		after := time.Now()
		// execute next on the nearest day
		time.Sleep(before.AddDate(0, 0, 1).Truncate(24 * time.Hour).Sub(after))
	}
}

// Len returns the number of files in the cache.
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

// SortedIDs returns a slice of every cached file's ID, sorted descending by modification
// time.
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

// SetDir sets the base directory where files will be stored on disk.
func (c *Cache) SetDir(dir string) {
	c.Lock()
	c.dir = dir
	c.Unlock()
}
