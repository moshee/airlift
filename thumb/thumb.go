// Package thumb implements a lazy image thumbnail cache. Supported input image
// formats are any format Go can decode natively from the standard library and
// subrepo golang.org/x/image.
package thumb

import (
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// Encoder describes a way to encode a thumbnail image.
type Encoder interface {
	Extension() string // The file extension of the resulting image
	Encode(dst io.Writer, thumb image.Image) error
}

type JPEGEncoder struct{ *jpeg.Options }

func (JPEGEncoder) Extension() string { return ".jpg" }
func (e JPEGEncoder) Encode(dst io.Writer, thumb image.Image) error {
	return jpeg.Encode(dst, thumb, e.Options)
}

// FileStore is a source of files that Cache will reference.
type FileStore interface {
	// Get should return the path to the file on disk, or the empty string if
	// not found.
	Get(id string) string
}

type size struct {
	w, h int
}

type set map[size]struct{}

type request struct {
	id string
	size
}

// Cache is a lazy, concurrent thumbnail cache for airlift-server with request
// batching for on-the-fly thumbnail generation. Only file paths are cached in
// memory.
type Cache struct {
	size     int64  // the total size of the thumbnails
	dir      string // path of directory where thumbnails are stored
	enc      Encoder
	store    FileStore
	files    map[string]set
	req      chan *request    // ID
	remove   chan string      // send ID, or empty string to purge all
	resp     chan interface{} // file path
	inflight map[string][]chan string
	scaler   draw.Scaler
}

// NewCache initializes a new thumbnail generator that stores files encoded
// from store by enc in dirPath. w and h determine the maximum dimensions of
// the thumbnails.
func NewCache(dirPath string, enc Encoder, store FileStore, scaler draw.Scaler) (*Cache, error) {
	c := &Cache{
		dir:      dirPath,
		enc:      enc,
		store:    store,
		files:    make(map[string]set),
		req:      make(chan *request),
		remove:   make(chan string),
		resp:     make(chan interface{}),
		inflight: make(map[string][]chan string),
		scaler:   scaler,
	}

	os.MkdirAll(dirPath, 0755)

	log.Print("thumb: loading cached thumbs...")
	i := 0
	filepath.Walk(dirPath, func(path string, fi os.FileInfo, err error) error {
		if fi.IsDir() {
			return nil
		}
		c.size += fi.Size()

		// format of filename: <path>_<width>_<height>.<ext>
		// chop off common prefix
		relpath, _ := filepath.Rel(dirPath, path)
		//
		relpathMinusExt := relpath[:len(relpath)-len(filepath.Ext(relpath))]
		j := 0

		// locate sizes by second to last
		sizesPos := strings.LastIndexFunc(relpathMinusExt, func(r rune) bool {
			if r == '_' {
				j++
				if j == 2 {
					return true
				}
			}
			return false
		})
		if sizesPos < 0 {
			log.Printf("thumb: filename '%s' has wrong format -- removing", relpath)
			os.Remove(path)
			return nil
		}

		sizes := relpathMinusExt[sizesPos+j:]
		s, err := parseSize(sizes)
		if err != nil {
			log.Printf("thumb: filename '%s' has wrong size format -- removing", relpath)
			os.Remove(path)
			return nil
		}

		id := relpathMinusExt[:sizesPos]
		c.addSize(id, s)
		i++
		return nil
	})

	log.Printf("thumb: loaded %d cached thumbnails", i)

	return c, nil
}

func parseSize(s string) (size, error) {
	sizes := strings.SplitN(s, "_", 2)
	if len(sizes) != 2 {
		return size{}, errors.New("invalid format")
	}
	w, err := strconv.Atoi(sizes[0])
	if err != nil {
		return size{}, err
	}
	h, err := strconv.Atoi(sizes[1])
	if err != nil {
		return size{}, err
	}
	return size{w, h}, nil
}

func (c *Cache) addSize(id string, s size) {
	set, ok := c.files[id]
	if !ok {
		set = make(map[size]struct{})
	}
	set[s] = struct{}{}
	c.files[id] = set
}

// Serve starts the cache request server, blocking forever. It should be
// launched in its own goroutine before any requests are made.
func (c *Cache) Serve() {
	for {
		select {
		case req := <-c.req:
			// check if a thumb of the requested size is already there
			if dims, ok := c.files[req.id]; ok {
				if _, ok := dims[req.size]; ok {
					// freshen thumb on request if original file changed
					origPath := c.store.Get(req.id)
					thumbPath := c.thumbPath(req)

					thumbFi, err := os.Stat(thumbPath)
					if err != nil {
						log.Print(err)
						c.getThumb(req)
						break
					}

					origFi, err := os.Stat(origPath)
					if err != nil {
						log.Print(err)
						c.getThumb(req)
						break
					}

					if origFi.ModTime().After(thumbFi.ModTime()) {
						c.getThumb(req)
						break
					}

					// serve existing thumb if already fresh
					ch := make(chan string)
					c.resp <- ch
					ch <- c.thumbPath(req)
					break
				}
			}
			c.getThumb(req)
		case id := <-c.remove:
			if id == "" {
				c.resp <- c.doPurge()
			} else {
				c.resp <- c.doRemove(id)
			}
		}
	}
}

func (c *Cache) Size() int64 {
	return c.size
}

func (c *Cache) thumbPath(req *request) string {
	basename := fmt.Sprintf("%s_%d_%d", req.id, req.w, req.h)
	return filepath.Join(c.dir, basename) + c.enc.Extension()
}

// Get the file path to the thumbnail of the file with the given id. Generate
// it if it doesn't exist already. If concurrent requests are made to the same
// non-existent thumbnail, it will only be generated once.
//
// TODO: error handling
func (c *Cache) Get(id string, w, h int) string {
	c.req <- &request{id, size{w, h}}
	resp := (<-c.resp).(chan string)
	return <-resp
}

func (c *Cache) getThumb(req *request) {
	ch := make(chan string, 1)
	c.resp <- ch
	c.inflight[req.id] = append(c.inflight[req.id], ch)
	// if there is a request happening on this already, simply add a reciever
	// to the list and let them wait for it
	if len(c.inflight[req.id]) > 1 {
		return
	}

	go func() {
		// now we enter the part of the function that actually does the work
		path := new(string)

		// once the work is done, send to all the recievers
		defer func() {
			for _, ch := range c.inflight[req.id] {
				ch <- *path
			}
			delete(c.inflight, req.id)
		}()

		src := c.store.Get(req.id)
		decoder := DecodeFunc(src)
		if decoder == nil {
			return
		}

		// generate thumb

		f, err := os.Open(src)
		if err != nil {
			log.Print("getThumb: ", err)
			return
		}

		p := c.thumbPath(req)
		os.MkdirAll(filepath.Dir(p), 0755)
		dst, err := os.Create(p)
		if err != nil {
			log.Print("getThumb: ", err)
			return
		}

		img, err := decoder(f)
		if err != nil {
			log.Print("getThumb: ", err)
			return
		}

		thumb := produceThumbnail(img, req.w, req.h, c.scaler)
		if err := c.enc.Encode(dst, thumb); err != nil {
			os.Remove(p)
			log.Print("getThumb: ", err)
			return
		}

		fi, err := dst.Stat()
		if err != nil {
			os.Remove(p)
			log.Print("getThumb: ", err)
			return
		}

		c.size += fi.Size()

		c.addSize(req.id, req.size)
		*path = p
	}()
}

func (c *Cache) Purge() error {
	c.remove <- ""
	err := <-c.resp
	if v, ok := err.(error); ok {
		return v
	}
	return nil
}

func (c *Cache) doPurge() error {
	for id := range c.files {
		if err := c.doRemove(id); err != nil {
			return err
		}
	}

	return nil
}

// Remove deletes all sizes of thumbnail of a given file.
func (c *Cache) Remove(id string) error {
	c.remove <- id
	err := <-c.resp
	if v, ok := err.(error); ok {
		return v
	}
	return nil
}

func (c *Cache) doRemove(id string) error {
	set, ok := c.files[id]
	if !ok {
		return nil
	}

	for s := range set {
		path := c.thumbPath(&request{id, s})
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		size := fi.Size()
		if err = os.Remove(path); err != nil {
			return err
		}
		c.size -= size
	}

	delete(c.files, id)

	return nil
}

var decodeFuncMap = map[string]func(io.Reader) (image.Image, error){
	".jpg":  jpeg.Decode,
	".jpeg": jpeg.Decode,
	".png":  png.Decode,
	".gif":  gif.Decode,
	".tif":  tiff.Decode,
	".tiff": tiff.Decode,
	".webp": webp.Decode,
	".bmp":  bmp.Decode,
}

// DecodeFunc returns a func that can be used to decode the image with the
// given file name, or nil if it's not supported.
// TODO: sniff magic number instead of only using file extension
// TODO: allow externally registered format decoders
func DecodeFunc(name string) func(io.Reader) (image.Image, error) {
	ext := strings.ToLower(filepath.Ext(name))
	return decodeFuncMap[ext]
}

// FormatSupported returns true if the given file extension belongs to an image
// format that can be thumbnailed by this package.
func FormatSupported(ext string) bool {
	ext = strings.ToLower(ext)
	for supportedExt := range decodeFuncMap {
		if ext == supportedExt {
			return true
		}
	}
	return false
}

func thumbDimensions(wDest, hDest, wSrc, hSrc int) (w, h int) {
	if wSrc > hSrc {
		w = wDest
		h = hSrc * wDest / wSrc
	} else {
		h = hDest
		w = wSrc * hDest / hSrc
	}

	return
}

func produceThumbnail(src image.Image, w, h int, s draw.Scaler) image.Image {
	wSrc, hSrc := src.Bounds().Dx(), src.Bounds().Dy()
	if wSrc <= w && hSrc <= h {
		return src
	}
	w, h = thumbDimensions(w, h, wSrc, hSrc)
	thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
	s.Scale(thumb, thumb.Bounds(), src, src.Bounds(), draw.Src, nil)
	return thumb
}
