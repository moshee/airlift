package main

import (
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nfnt/resize"
)

const (
	placeholderThumb = "/static/file.svg"
	thumbWidth       = 100
	thumbHeight      = 100
	thumbCachePath   = "thumb-cache"
)

func thumbnailFunc(name string) func(io.Reader) (image.Image, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg":
		return jpeg.Decode
	case ".png":
		return png.Decode
	case ".gif":
		return gif.Decode
	default:
		return nil
	}
}

// TODO: make this more concurrent
func thumbPath(id string) string {
	if s := thumbCache.get(id); s != "" {
		return s
	}

	thumbCache.Lock()
	defer thumbCache.Unlock()
	var (
		dstName  = id + ".jpg"
		fileName = fileList.get(id)
		filePath = filepath.Join(fileList.Base, fileName)
		f, err   = os.Open(filePath)
		img      image.Image
	)

	if err != nil {
		return ""
	}

	fn := thumbnailFunc(fileName)
	if fn == nil {
		return ""
	}

	img, err = fn(f)
	if err != nil {
		log.Print("thumbnail: ", err)
		return ""
	}

	os.MkdirAll(thumbCache.Base, 0755)
	dstPath := filepath.Join(thumbCache.Base, dstName)
	dst, err := os.Create(dstPath)
	if err != nil {
		log.Print("thumbnail: ", err)
		return ""
	}

	thumb := resize.Thumbnail(thumbWidth, thumbHeight, img, resize.Bilinear)
	if err := jpeg.Encode(dst, thumb, &jpeg.Options{88}); err != nil {
		os.Remove(dstPath)
		log.Print("thumbnail: ", err)
		return ""
	}

	fi, err := dst.Stat()
	if err != nil {
		os.Remove(dstPath)
		log.Print("thumbnail: ", err)
		return ""
	}

	thumbCache.Size += fi.Size()
	thumbCache.Files[id] = fi

	return dstName
}
