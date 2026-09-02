// Command bunyip-pack bundles an asset directory into a pack file that
// asset.Open can read alongside, or instead of, loose directories.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/matjam/bunyip/asset"
)

func main() {
	out := flag.String("o", "assets.pak", "pack file to write")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: bunyip-pack [-o assets.pak] directory")
		os.Exit(2)
	}
	if err := asset.Pack(flag.Arg(0), *out); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-pack:", err)
		os.Exit(1)
	}
	info, err := os.Stat(*out)
	if err == nil {
		fmt.Printf("wrote %s (%d bytes)\n", *out, info.Size())
	}
}
