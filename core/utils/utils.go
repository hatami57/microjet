// Package utils provides small, dependency-free helpers: generic pointer
// coalescing plus environment-variable and disk-space utilities.
package utils

func Coalesce[T any](ptrs ...*T) *T {
	for _, p := range ptrs {
		if p != nil {
			return p
		}
	}
	return nil
}

func CoalesceVal[T any](def T, ptrs ...*T) T {
	for _, p := range ptrs {
		if p != nil {
			return *p
		}
	}
	return def
}
