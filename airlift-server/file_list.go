package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

type FileList struct {
	Files map[string]os.FileInfo
	Size  int64
	Base  string
	sync.RWMutex
}

func (files *FileList) get(id string) string {
	files.RLock()
	defer files.RUnlock()
	file, ok := files.Files[id]
	if !ok {
		return ""
	}
	return file.Name()
}

// put creates a temp file, downloads a post body to it, moves it to the
// FileList root, adds the file to the in-memory list, and returns the
// generated hash.
func (files *FileList) put(conf *Config, content io.Reader, filename string) (string, error) {
	os.MkdirAll(files.Base, 0644)
	dest := filepath.Join(files.Base, filename)
	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		os.Remove(dest)
		return "", err
	}

	defer destFile.Close()

	sha := sha3.New256()
	w := io.MultiWriter(destFile, sha)
	io.Copy(w, content)
	hash := makeHash(sha.Sum(nil))

	if existing, exist := files.Files[hash]; exist {
		base := filepath.Base(existing.Name())
		os.Remove(filepath.Join(files.Base, base))
	}

	destPath := filepath.Join(files.Base, hash+"."+filename)
	if err := os.Rename(dest, destPath); err != nil {
		os.Remove(dest)
		return "", err
	}

	fi, err := os.Stat(destPath)
	if err != nil {
		os.Remove(dest)
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
	ids := files.sortedIds()
	pruned := int64(0)
	n := 0
	for i := 0; files.Size > conf.MaxSize*1024*1024 && i < len(ids); i++ {
		id := ids[i]
		f := files.Files[id]
		if err := files.remove(id); err != nil {
			log.Printf("pruning %s: %v", f.Name(), err)
			continue
		}
		pruned += f.Size()
		n++
	}
	if n > 0 {
		log.Printf("Pruned %d uploads (%.2fMB) to keep under %dMB",
			n, float64(pruned)/(1024*1024), conf.MaxSize)
	}
}

func (files *FileList) pruneNewest() (string, error) {
	files.Lock()
	defer files.Unlock()

	if len(files.Files) == 0 {
		return "", nil
	}

	var newest os.FileInfo
	newestId := ""

	for id, fi := range files.Files {
		if newest == nil {
			newest = fi
		}
		if fi.ModTime().After(newest.ModTime()) {
			newest = fi
			newestId = id
		}
	}

	return newestId, files.remove(newestId)
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

func (files *FileList) watchAges() {
	for {
		conf := <-configChan
		before := time.Now()
		if conf.MaxAge > 0 {
			cutoff := before.Add(-time.Duration(conf.MaxAge) * 24 * time.Hour)
			files.pruneOld(cutoff)
		}
		after := time.Now()
		// execute next on the nearest day
		time.Sleep(before.AddDate(0, 0, 1).Truncate(24 * time.Hour).Sub(after))
	}
}

func (files *FileList) pruneOld(cutoff time.Time) {
	files.Lock()
	defer files.Unlock()
	n := 0
	for id, fi := range files.Files {
		if fi.ModTime().Before(cutoff) {
			if err := files.remove(id); err != nil {
				log.Printf("Error pruning %s: %v", fi.Name(), err)
				continue
			}
		}
	}
	if n > 0 {
		log.Printf("%d upload(s) modified before %s pruned.", n, cutoff.Format("2006-01-02"))
	}
}

func (files *FileList) remove(id string) error {
	fi, ok := files.Files[id]
	if !ok {
		return fmt.Errorf("File id %s doesn't exist", id)
	}

	name := filepath.Join(files.Base, fi.Name())
	err := os.Remove(name)
	if err != nil {
		return err
	}

	files.Size -= fi.Size()
	delete(files.Files, id)
	return nil
}

func (files *FileList) sortedIds() []string {
	ids := make([]string, 0, len(files.Files))
	for id := range files.Files {
		ids = append(ids, id)
	}

	sort.Sort(byModtime(ids))
	return ids
}

func (files *FileList) purge() error {
	files.Lock()
	defer files.Unlock()

	for id, fi := range files.Files {
		path := filepath.Join(files.Base, fi.Name())
		s := fi.Size()
		if err := os.Remove(path); err != nil {
			return err
		}
		files.Size -= s
		delete(files.Files, id)
	}

	return nil
}
