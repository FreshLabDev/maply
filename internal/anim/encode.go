// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Options tune the encoding. The zero value is the posts' setting.
type Options struct {
	Workers int    // frames drawn at once; 0 is one per CPU
	CRF     int    // x264 quality, lower is better; 0 is 22
	Preset  string // x264 preset; "" is medium
	FFmpeg  string // the ffmpeg binary; "" is ffmpeg on PATH
}

// Stats is what an encoding took.
type Stats struct {
	Frames int
	Draw   time.Duration // summed over the workers
	Total  time.Duration
}

// Encode draws every frame of one loop of the scene and pipes them to ffmpeg,
// which writes an H.264 MP4 to out: what Telegram plays as a GIF. Frames are
// drawn in parallel and written in order.
func Encode(ctx context.Context, s Scene, out string, o Options) (Stats, error) {
	start := time.Now()
	w, h := s.Size()
	if o.Workers <= 0 {
		o.Workers = runtime.GOMAXPROCS(0)
	}
	if o.CRF == 0 {
		o.CRF = 22
	}
	if o.Preset == "" {
		o.Preset = "medium"
	}
	if o.FFmpeg == "" {
		o.FFmpeg = "ffmpeg"
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.FFmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "-s", fmt.Sprintf("%dx%d", w, h), "-r", strconv.Itoa(FPS), "-i", "pipe:0",
		"-an", "-c:v", "libx264", "-preset", o.Preset, "-crf", strconv.Itoa(o.CRF),
		"-vf", "scale=out_color_matrix=bt709:out_range=tv", "-pix_fmt", "yuv420p",
		"-x264-params", "colorprim=bt709:transfer=bt709:colormatrix=bt709:range=tv",
		"-movflags", "+faststart", out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Stats{}, err
	}
	if err := cmd.Start(); err != nil {
		return Stats{}, fmt.Errorf("start ffmpeg: %w", err)
	}
	st, werr := frames(ctx, s, o.Workers, stdin)
	stdin.Close()
	if werr != nil {
		cancel()
		cmd.Wait()
		return st, werr
	}
	if err := cmd.Wait(); err != nil {
		return st, fmt.Errorf("ffmpeg: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	st.Total = time.Since(start)
	return st, nil
}

// frames draws the loop with workers goroutines and writes the frames to w as
// raw RGB in order. At most two frames per worker wait to be written.
func frames(ctx context.Context, s Scene, workers int, w io.Writer) (Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	n := Frames(s)
	width, height := s.Size()
	size := 3 * width * height
	type frame struct {
		i   int
		buf []byte
	}
	pool := sync.Pool{New: func() any { return make([]byte, size) }}
	jobs := make(chan int)
	done := make(chan frame, 2*workers)
	slots := make(chan struct{}, 2*workers)
	var drawn time.Duration
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := NewCanvas(width, height)
			for i := range jobs {
				t0 := time.Now()
				s.Draw(c, float64(i)/FPS)
				buf := pool.Get().([]byte)
				c.RGB24(buf)
				mu.Lock()
				drawn += time.Since(t0)
				mu.Unlock()
				done <- frame{i, buf}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < n; i++ {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(done) }()

	waiting := map[int][]byte{}
	next := 0
	var err error
	for f := range done {
		waiting[f.i] = f.buf
		for buf, ok := waiting[next]; ok; buf, ok = waiting[next] {
			delete(waiting, next)
			if err == nil {
				if _, werr := w.Write(buf); werr != nil {
					err = fmt.Errorf("write frame %d: %w", next, werr)
					cancel()
				}
			}
			pool.Put(buf)
			<-slots
			next++
		}
	}
	if err == nil && next != n {
		err = errors.Join(ctx.Err(), fmt.Errorf("wrote %d of %d frames", next, n))
	}
	return Stats{Frames: next, Draw: drawn}, err
}

// Still draws one frame, for previews and tests.
func Still(s Scene, t float64) *image.RGBA {
	w, h := s.Size()
	c := NewCanvas(w, h)
	s.Draw(c, t)
	return c.Image()
}
