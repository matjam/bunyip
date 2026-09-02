package asset_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	"github.com/matjam/bunyip/asset"
)

func ExampleFSSource() {
	// A game embeds its assets with go:embed and opens them behind an
	// optional loose directory so edits on disk win while developing.
	// Any io/fs.FS works; a MapFS stands in for the embed.FS here.
	embedded := fstest.MapFS{
		"text/intro.txt": {Data: []byte("Once upon a time")},
		"text/end.txt":   {Data: []byte("The end")},
	}
	sources := []asset.Source{asset.FSSource(embedded)}
	if _, err := os.Stat("assets"); err == nil {
		sources = append([]asset.Source{asset.Dir("assets")}, sources...)
	}
	fs, err := asset.OpenFS(sources...)
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	data, _ := fs.Read("text/end.txt")
	fmt.Println(string(data))
	fmt.Println(fs.List("text"))
	fmt.Printf("%q\n", fs.Path("text/end.txt"))
	// Output:
	// The end
	// [text/end.txt text/intro.txt]
	// ""
}

func Example() {
	// A directory of loose files while developing; ship a pack file built
	// with bunyip-pack and open both, directory first, so loose files win.
	dir, _ := os.MkdirTemp("", "asset-example")
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "text"), 0o755)
	os.WriteFile(filepath.Join(dir, "text", "intro.txt"), []byte("Once upon a time"), 0o644)
	asset.Pack(dir, filepath.Join(dir, "..", "example.pak"))
	defer os.Remove(filepath.Join(dir, "..", "example.pak"))

	fs, err := asset.Open(dir, filepath.Join(dir, "..", "example.pak"))
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	data, _ := fs.Read("text/intro.txt")
	fmt.Println(string(data))
	fmt.Println(fs.List(""))

	// Decode on worker goroutines; poll Ready from the game loop, or Wait.
	loader := asset.NewLoader(fs, 0)
	defer loader.Close()
	upper := asset.Load(loader, "text/intro.txt", func(b []byte) (string, error) { return strings.ToUpper(string(b)), nil })
	v, _ := upper.Get()
	fmt.Println(v)
	// Output:
	// Once upon a time
	// [text/intro.txt]
	// ONCE UPON A TIME
}
