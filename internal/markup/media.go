package markup

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Options struct {
	BaseDir             string
	Media, RemoteImages bool
	Cache               *MediaCache
}

// Cache only the small terminal representation, never retained full bitmaps.
// One cache belongs to one preview; an explicit refresh replaces it.
type MediaCache struct {
	mu      sync.Mutex
	entries map[string]Document
}

func (c *MediaCache) get(key string) (Document, bool) {
	if c == nil {
		return Document{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.entries[key]
	return d, ok
}
func (c *MediaCache) put(key string, d Document) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= 16 || c.entries == nil {
		c.entries = map[string]Document{}
	}
	c.entries[key] = d
}

const maxImageBytes = 8 << 20
const maxImagePixels = 4 << 20

func (r *renderer) image(target string) {
	if r.assets >= 8 {
		r.write(" [image limit: eight per preview]", Style{Dim: true})
		return
	}
	r.assets++
	d, err := r.loadImage(target)
	if err != nil {
		r.write(" [image: "+err.Error()+"]", Style{Dim: true})
		return
	}
	r.hasMedia = true
	r.newline(1)
	child := &renderer{ctx: r.ctx, spans: d.Spans}
	child.out.WriteString(d.Text)
	r.append(child)
	r.newline(1)
}

func (r *renderer) loadImage(target string) (Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return Document{}, fmt.Errorf("invalid destination")
	}
	remote := u.Scheme == "http" || u.Scheme == "https"
	key := target
	var path string
	if remote {
		if !r.options.RemoteImages {
			return Document{}, fmt.Errorf("remote; use Load remote preview images")
		}
		if SafeURL(target) == "" {
			return Document{}, fmt.Errorf("unsupported URL")
		}
	} else {
		if LinkTarget(target) == "" || u.Scheme != "" || u.Path == "" {
			return Document{}, fmt.Errorf("unsupported destination")
		}
		if r.options.BaseDir == "" {
			return Document{}, fmt.Errorf("save the source to resolve this path")
		}
		path = filepath.Join(r.options.BaseDir, filepath.FromSlash(u.Path))
		info, e := os.Stat(path)
		if e != nil {
			return Document{}, fmt.Errorf("file unavailable")
		}
		if !info.Mode().IsRegular() || info.Size() > maxImageBytes {
			return Document{}, fmt.Errorf("requires a regular file up to 8 MiB")
		}
		key = fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
	}
	if cached, ok := r.options.Cache.get(key); ok {
		return cached, nil
	}
	if err = r.ctx.Err(); err != nil {
		return Document{}, err
	}
	var reader io.ReadCloser
	if remote {
		ctx, cancel := context.WithDeadline(r.ctx, r.assetDeadline)
		defer cancel()
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if e != nil {
			return Document{}, e
		}
		client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || SafeURL(req.URL.String()) == "" {
				return fmt.Errorf("redirect limit or unsupported destination")
			}
			return nil
		}}
		response, e := client.Do(request)
		if e != nil {
			return Document{}, fmt.Errorf("download failed or timed out")
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return Document{}, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		reader = response.Body
	} else {
		f, e := os.Open(path)
		if e != nil {
			return Document{}, fmt.Errorf("file unavailable")
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return Document{}, fmt.Errorf("not a regular image file")
		}
		reader = f
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		return Document{}, fmt.Errorf("image read failed or exceeds 8 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Document{}, fmt.Errorf("supported formats: PNG, JPEG and GIF")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return Document{}, fmt.Errorf("image exceeds four megapixels")
	}
	bitmap, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Document{}, fmt.Errorf("image decode failed")
	}
	child := &renderer{ctx: r.ctx}
	w := min(48, cfg.Width)
	h := max(1, cfg.Height*w/cfg.Width)
	if h > 48 {
		w = max(1, w*48/h)
		h = 48
	}
	bounds := bitmap.Bounds()
	// Half blocks encode two source pixels per terminal cell. Alpha is blended
	// against a neutral checkerboard, preserving visibility in either theme.
	pixel := func(x, y int) uint32 {
		x = min(cfg.Width-1, x*cfg.Width/w)
		y = min(cfg.Height-1, y*cfg.Height/h)
		red, green, blue, alpha := bitmap.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
		bg := uint32(0x3333)
		if (x/8+y/8)%2 == 0 {
			bg = 0x7777
		}
		blend := func(v uint32) uint32 { return (v + bg*(0xffff-alpha)/0xffff) >> 8 }
		return blend(red)<<16 | blend(green)<<8 | blend(blue)
	}
	for y := 0; y < h; y += 2 {
		if err = r.ctx.Err(); err != nil {
			return Document{}, err
		}
		for x := 0; x < w; x++ {
			child.write("▀", Style{Pixel: true, Foreground: pixel(x, y), Background: pixel(x, min(h-1, y+1))})
		}
		child.write("\n", Style{})
	}
	d := Document{Text: child.out.String(), Spans: child.spans}
	r.options.Cache.put(key, d)
	return d, nil
}
