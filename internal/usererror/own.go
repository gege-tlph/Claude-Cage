package usererror

// Own returns err with its Failure copied when err is one, its tools in a
// slice of their own. Completing a failure (its lead, its tools, the node it
// names) must not change a value that another call returns too, such as a
// package-level failure. A wrapper is returned as it is: it was made for
// this error.
func Own(err error) error {
	f, ok := err.(*Failure)
	if !ok || f == nil {
		return err
	}
	copied := *f
	copied.Tools = append([]string(nil), f.Tools...)
	return &copied
}
