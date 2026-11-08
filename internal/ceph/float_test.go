package ceph

import (
	"encoding/json"
	"math"
	"testing"
)

func TestFloatMarshalJSON(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1, `{"weight":1.0}`},
		{0, `{"weight":0.0}`},
		{0.8, `{"weight":0.8}`},
		{0.0098, `{"weight":0.0098}`},
		{2, `{"weight":2.0}`},
		{1e-7, `{"weight":0.0000001}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(map[string]any{"weight": Float(c.in)})
		if err != nil {
			t.Fatalf("Marshal(%v): %v", c.in, err)
		}
		if string(got) != c.want {
			t.Errorf("Marshal(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestFloatMarshalJSONRejectsNaNInf(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := json.Marshal(Float(v)); err == nil {
			t.Errorf("Marshal(%v): expected an error", v)
		}
	}
}
