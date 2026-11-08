package ceph

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Float is a mon-command argument that Ceph declares as a float (CephFloat),
// such as the weight of `osd reweight` or `osd crush reweight`.
//
// Ceph rejects an integral JSON number for a float argument: `"weight": 1`
// fails with EINVAL ("bad or missing field 'weight'"), while `"weight": 1.0`
// is accepted. Go's encoding/json writes float64(1) as `1`, so a plain float64
// breaks every whole-number value (e.g. reweighting an OSD back to 1.00).
// Float always encodes with a decimal point.
type Float float64

// MarshalJSON encodes f as a JSON number that always contains a decimal point
// (1 → 1.0, 0.8 → 0.8). NaN and ±Inf are not valid JSON and return an error.
func (f Float) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("ceph: invalid float argument %v", v)
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return []byte(s), nil
}
