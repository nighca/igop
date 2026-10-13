package ixgo

import (
	"reflect"
	"testing"
)

func BenchmarkFrameCalls(b *testing.B) {
	for _, tc := range []struct {
		name string
		args []Value
		want Value
	}{
		{"empty", nil, nil},
		{"add", []Value{41}, 42},
		{"arithmetic", []Value{1000}, 499500},
		{"pairLoop", []Value{1000}, 1000000},
		{"pair2Loop", []Value{1000}, 999000},
		{"fibish", []Value{20}, Tuple{2584, 8362}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ctx := NewContext(0)
			ctx.SetLeastCallForEnablePool(0)
			interp, err := ctx.LoadInterp("frame_bench.go", frameCallsSource)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(interp.UnsafeRelease)
			if got, err := interp.RunFunc(tc.name, tc.args...); err != nil || !reflect.DeepEqual(got, tc.want) {
				b.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := interp.RunFunc(tc.name, tc.args...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

const frameCallsSource = `package main
func empty() {}
func add(n int) int { return n + 1 }
func pair(n int) (int, int) { return n, n + 1 }
func arithmetic(n int) int {
	s := 0
	for i := 0; i < n; i++ { s += i }
	return s
}
func pairLoop(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		a, b := pair(i)
		s += a + b
	}
	return s
}
func pair2Loop(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		a, b := pair2(i, i + 1)
		s += a + b
	}
	return s
}
func pair2(a, b int) (int, int) { return a + b, a - b }
func fibish(x int) (int, int) {
	if x < 3 { return 0, x }
	a, b := fibish(x - 2)
	c, d := fibish(x - 1)
	return a + d, b + c
}
`
