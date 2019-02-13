package main

import (
	"encoding/json"
	"io"

	"github.com/XuebingZhao/3/data"
)

func dumpJSON(f *data.Slice, info data.Meta, out io.Writer) {
	w := json.NewEncoder(out)
	w.Encode(f.Tensors())
}
