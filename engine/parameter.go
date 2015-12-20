package engine

/*
parameters are region- and time dependent input values,
like material parameters.
*/

import (
	"github.com/mumax/3/data"
	"github.com/mumax/3/script"
	"github.com/mumax/3/util"
	"math"
	"reflect"
	"strings"
)

// input parameter, settable by user
type param struct {
	lut
	upd_reg    [NREGION]func() []float64 // time-dependent values
	timestamp  float64                   // used not to double-evaluate f(t)
	children   []derived                 // derived parameters
	name, unit string
}

func (p *param) init(nComp int, name, unit string, children []derived) {
	p.lut.init(nComp, p)
	p.name = name
	p.unit = unit
	p.children = children
	p.timestamp = math.Inf(-1)
}

func (p *param) Name() string     { return p.name }
func (p *param) Unit() string     { return p.unit }
func (p *param) Mesh() *data.Mesh { return Mesh() }

func (p *param) update() {
	if p.timestamp != Time {
		changed := false
		// update functions of time
		for r := 0; r < NREGION; r++ {
			updFunc := p.upd_reg[r]
			if updFunc != nil {
				p.bufset_(r, updFunc())
				changed = true
			}
		}
		p.timestamp = Time
		if changed {
			p.invalidate()
		}
	}
}

// set in one region
func (p *param) setRegion(region int, v []float64) {
	if region == -1 {
		p.setUniform(v)
	} else {
		p.setRegions(region, region+1, v)
	}
}

// set in all regions
func (p *param) setUniform(v []float64) {
	p.setRegions(0, NREGION, v)
}

// set in regions r1..r2(excl)
func (p *param) setRegions(r1, r2 int, v []float64) {
	util.Argument(len(v) == len(p.cpu_buf))
	util.Argument(r1 < r2) // exclusive upper bound
	for r := r1; r < r2; r++ {
		p.upd_reg[r] = nil
		p.bufset_(r, v)
	}
	p.invalidate()
}

func (p *param) bufset_(region int, v []float64) {
	for c := range p.cpu_buf {
		p.cpu_buf[c][region] = float32(v[c])
	}
}

func (p *param) setFunc(r1, r2 int, f func() []float64) {
	util.Argument(r1 < r2) // exclusive upper bound
	for r := r1; r < r2; r++ {
		p.upd_reg[r] = f
	}
	p.invalidate()
}

// mark my GPU copy and my children as invalid (need update)
func (p *param) invalidate() {
	p.gpu_ok = false
	for _, c := range p.children {
		c.invalidate()
	}
}

func (p *param) getRegion(region int) []float64 {
	cpu := p.cpuLUT()
	v := make([]float64, p.NComp())
	for i := range v {
		v[i] = float64(cpu[i][region])
	}
	return v
}

func (p *param) IsUniform() bool {
	cpu := p.cpuLUT()
	v1 := p.getRegion(0)
	for r := 1; r < NREGION; r++ {
		for c := range v1 {
			if cpu[c][r] != float32(v1[c]) {
				return false
			}
		}
	}
	return true
}

func (p *param) average() []float64 { return qAverageUniverse(p) }

// parameter derived from others (not directly settable). E.g.: Bsat derived from Msat
type derivedParam struct {
	lut                          // GPU storage
	updater  func(*derivedParam) // called to update my value
	uptodate bool                // cleared if parents' value change
	parents  []updater           // parents updated before I'm updated
}

// any parameter that depends on an inputParam
type derived interface {
	invalidate()
}

func (p *derivedParam) init(nComp int, parents []updater, updater func(*derivedParam)) {
	p.lut.init(nComp, p) // pass myself to update me if needed
	p.updater = updater
	p.parents = parents
}

func (p *derivedParam) invalidate() {
	p.uptodate = false
}

func (p *derivedParam) update() {
	for _, par := range p.parents {
		par.update() // may invalidate me
	}
	if !p.uptodate {
		p.updater(p)
		p.gpu_ok = false
		p.uptodate = true
	}
}

// Get value in region r.
func (p *derivedParam) GetRegion(r int) []float64 {
	lut := p.cpuLUT() // updates me if needed
	v := make([]float64, p.NComp())
	for c := range v {
		v[c] = float64(lut[c][r])
	}
	return v
}

// specialized param with 1 component
type ScalarParam struct {
	param
}

func (p *ScalarParam) init(name, unit, desc string, children []derived) {
	p.param.init(SCALAR, name, unit, children)
	DeclLValue(name, p, cat(desc, unit))
}

func NewScalarParam(name, unit, desc string, children []derived) *ScalarParam {
	p := new(ScalarParam)
	p.param.init(SCALAR, name, unit, children)
	DeclLValue(name, p, cat(desc, unit))
	return p
}

func (p *ScalarParam) SetRegion(region int, f script.ScalarFunction) {
	if region == -1 {
		p.setRegionsFunc(0, NREGION, f) // uniform
	} else {
		p.setRegionsFunc(region, region+1, f) // upper bound exclusive
	}
}

func (p *ScalarParam) SetValue(v interface{}) {
	f := v.(script.ScalarFunction)
	p.setRegionsFunc(0, NREGION, f)
}

func (p *ScalarParam) Set(v float64) {
	p.setRegions(0, NREGION, []float64{v})
}

func (p *ScalarParam) setRegionsFunc(r1, r2 int, f script.ScalarFunction) {
	if Const(f) {
		p.setRegions(r1, r2, []float64{f.Float()})
	} else {
		f := f.Fix() // fix values of all variables except t
		p.setFunc(r1, r2, func() []float64 {
			return []float64{f.Eval().(script.ScalarFunction).Float()}
		})
	}
}

func (p *ScalarParam) GetRegion(region int) float64 {
	return float64(p.getRegion(region)[0])
}

func (p *ScalarParam) Eval() interface{}       { return p }
func (p *ScalarParam) Type() reflect.Type      { return reflect.TypeOf(new(ScalarParam)) }
func (p *ScalarParam) InputType() reflect.Type { return script.ScalarFunction_t }
func (p *ScalarParam) Average() float64        { return qAverageUniverse(p)[0] }
func (p *ScalarParam) Region(r int) *sOneReg   { return sOneRegion(p, r) }

// checks if a script expression contains t (time)
func Const(e script.Expr) bool {
	t := World.Resolve("t")
	return !script.Contains(e, t)
}

func cat(desc, unit string) string {
	if unit == "" {
		return desc
	} else {
		return desc + " (" + unit + ")"
	}
}

// these methods should only be accesible from Go

func (p *ScalarParam) SetRegionValueGo(region int, v float64) {
	if region == -1 {
		p.setRegions(0, NREGION, []float64{v})
	} else {
		p.setRegions(region, region+1, []float64{v})
	}
}

func (p *ScalarParam) SetRegionFuncGo(region int, f func() float64) {
	if region == -1 {
		p.setFunc(0, NREGION, func() []float64 {
			return []float64{f()}
		})
	} else {
		p.setFunc(region, region+1, func() []float64 {
			return []float64{f()}
		})
	}
}

// vector input parameter, settable by user
type VectorParam struct {
	param
}

func (p *VectorParam) init(name, unit, desc string) {
	p.param.init(VECTOR, name, unit, nil) // no vec param has children (yet)
	if !strings.HasPrefix(name, "_") {    // don't export names beginning with "_" (e.g. from exciation)
		DeclLValue(name, p, cat(desc, unit))
	}
}

func (p *VectorParam) SetRegion(region int, f script.VectorFunction) {
	if region == -1 {
		p.setRegionsFunc(0, NREGION, f) //uniform
	} else {
		p.setRegionsFunc(region, region+1, f)
	}
}

func (p *VectorParam) SetValue(v interface{}) {
	f := v.(script.VectorFunction)
	p.setRegionsFunc(0, NREGION, f)
}

func (p *VectorParam) setRegionsFunc(r1, r2 int, f script.VectorFunction) {
	if Const(f) {
		p.setRegions(r1, r2, slice(f.Float3()))
	} else {
		f := f.Fix() // fix values of all variables except t
		p.setFunc(r1, r2, func() []float64 {
			return slice(f.Eval().(script.VectorFunction).Float3())
		})
	}
}

func (p *VectorParam) SetRegionFn(region int, f func() [3]float64) {
	p.setFunc(region, region+1, func() []float64 {
		return slice(f())
	})
}

func (p *VectorParam) GetRegion(region int) [3]float64 {
	v := p.getRegion(region)
	return unslice(v)
}

func (p *VectorParam) Eval() interface{}       { return p }
func (p *VectorParam) Type() reflect.Type      { return reflect.TypeOf(new(VectorParam)) }
func (p *VectorParam) InputType() reflect.Type { return script.VectorFunction_t }
func (p *VectorParam) Region(r int) *vOneReg   { return vOneRegion(p, r) }
func (p *VectorParam) Average() data.Vector    { return unslice(qAverageUniverse(p)) }
func (p *VectorParam) Comp(c int) ScalarField  { return Comp(p, c) }