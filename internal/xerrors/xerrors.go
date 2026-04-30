// Package xerrors holds tiny error-handling helpers shared across packages.
package xerrors

// First returns the first non-nil error in errs, or nil if all are nil.
// Handy for plumbing multiple Close/Wait/Run returns where only the earliest
// failure matters.
func First(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
