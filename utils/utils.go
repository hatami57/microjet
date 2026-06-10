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
