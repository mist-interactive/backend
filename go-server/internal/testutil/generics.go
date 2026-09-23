package testutil

// Ptr returns a pointer to the passed value. Convenient for constructing test models with pointer fields.
func Ptr[T any](v T) *T {
	return &v
}
