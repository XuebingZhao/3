package engine

import (
	"code.google.com/p/mx3/cuda"
	"code.google.com/p/mx3/data"
	"code.google.com/p/mx3/util"
	"log"
)

type Regions struct {
	gpu        *cuda.Bytes
	cpu        []byte
	arr        [][][]byte
	cache      *data.Slice
	cacheValid bool
	autosave
}

func (r *Regions) init() {
	r.gpu = cuda.NewBytes(Mesh())
	r.cpu = make([]byte, r.gpu.Len)
	r.arr = resizeBytes(r.cpu, r.gpu.Mesh().Size())
	r.rasterGeom()
	r.autosave.nComp = 1
	r.autosave.name = "regions"
	r.autosave.mesh = r.gpu.Mesh()
}

func (r *Regions) upload() {
	r.gpu.Upload(r.cpu)
	r.cacheValid = false // upload indicates arr has changed, so cache probably invalid
}

func DefRegion(id int, s Shape) {
	if id < 0 || id > 255 {
		log.Fatalf("region id should be 0-255, have: %v", id)
	}
	n := Mesh().Size()
	c := Mesh().CellSize()
	dx := (float64(n[2]/2) - 0.5) * c[2]
	dy := (float64(n[1]/2) - 0.5) * c[1]
	dz := (float64(n[0]/2) - 0.5) * c[0]

	for i := 0; i < n[0]; i++ {
		z := float64(i)*c[0] - dz
		for j := 0; j < n[1]; j++ {
			y := float64(j)*c[1] - dy
			for k := 0; k < n[2]; k++ {
				x := float64(k)*c[2] - dx
				if s(x, y, z) { // inside
					regions.arr[i][j][k] = byte(id)
				}
			}
		}
	}
	regions.upload()
	M.stencilGeom() // TODO: revise if really needed
}

// Rasterises the global geom shape
func (r *Regions) rasterGeom() {
	s := geom
	if s == nil {
		s = universe
	}
}

func (r *Regions) Mesh() *data.Mesh { return r.gpu.Mesh() }

func (r *Regions) Get() (*data.Slice, bool) {
	if !r.cacheValid {
		if r.cache == nil {
			r.cache = data.NewSlice(1, r.Mesh())
		}
		l := r.cache.Host()[0]
		for i := range l {
			l[i] = float32(r.cpu[i])
		}
		log.Println("caching regions output")
		r.cacheValid = true
	}
	return r.cache, false
}

// Re-interpret a contiguous array as a multi-dimensional array of given size.
func resizeBytes(array []byte, size [3]int) [][][]byte {
	Nx, Ny, Nz := size[0], size[1], size[2]
	util.Argument(Nx*Ny*Nz == len(array))
	sliced := make([][][]byte, Nx)
	for i := range sliced {
		sliced[i] = make([][]byte, Ny)
	}
	for i := range sliced {
		for j := range sliced[i] {
			sliced[i][j] = array[(i*Ny+j)*Nz+0 : (i*Ny+j)*Nz+Nz]
		}
	}
	return sliced
}