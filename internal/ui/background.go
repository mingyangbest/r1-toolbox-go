package ui

import (
	"image"
	"image/jpeg"
	"os"
	"github.com/veandco/go-sdl2/sdl"
)

func LoadBackground(path string, width, height int32) (*sdl.Surface, error) {
	if path == "" {
		return nil, nil
	}
	
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	
	return imageToSurface(img)
}

func imageToSurface(img image.Image) (*sdl.Surface, error) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	
	surface, err := sdl.CreateRGBSurface(0, int32(w), int32(h), 32, 0, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	
	surface.Lock()
	defer surface.Unlock()
	
	pixels := surface.Pixels()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			idx := (y*w + x) * 4
			pixels[idx] = byte(b >> 8)
			pixels[idx+1] = byte(g >> 8)
			pixels[idx+2] = byte(r >> 8)
			pixels[idx+3] = byte(a >> 8)
		}
	}
	
	return surface, nil
}

func SaveImage(img image.Image, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	
	return jpeg.Encode(file, img, &jpeg.Options{Quality: 90})
}
