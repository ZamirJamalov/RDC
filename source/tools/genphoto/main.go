// genphoto — PR #409 üçün nümunə müştəri şəkli placeholder-i yaradır.
// MÜVƏQQƏTİ alət: `go run ./tools/genphoto web/assets/sample-customer-photo.jpg`
// PR #410-da bu alət və yaratdığı fayl fallback ilə birlikdə silinəcək.
package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
)

func main() {
	w, h := 300, 384
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// arxa fon: yumşaq qradiyent (ID şəkli fonu təəssüratı)
			c := color.RGBA{uint8(205 - y/3), uint8(205 - y/4), uint8(215 - y/5), 255}
			dx, dy := x-150, y-150
			if dx*dx+dy*dy < 62*62 {
				c = color.RGBA{112, 122, 132, 255}
			}
			dx2, dy2 := x-150, y-335
			if dx2*dx2*4900+dy2*dy2*14400 < 70560000 {
				c = color.RGBA{92, 102, 117, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		panic(err)
	}
}
