package gormx

import (
	"errors"

	"gorm.io/gorm"
)

// Constraint-violation classifiers. The postgres and sqlite drivers open with
// gorm's TranslateError enabled (see the driver Open funcs), so GORM maps
// driver-specific errors to its portable sentinels. These helpers let callers
// classify a failure without importing gorm directly or sniffing driver-specific
// error codes (e.g. pg SQLSTATE 23505).

// IsDuplicateKey reports whether err is a unique-constraint (duplicate key)
// violation — e.g. inserting a row whose unique key already exists.
func IsDuplicateKey(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

// IsForeignKeyViolation reports whether err is a foreign-key constraint violation
// — e.g. referencing a parent row that does not exist, or deleting one still
// referenced under ON DELETE RESTRICT.
func IsForeignKeyViolation(err error) bool {
	return errors.Is(err, gorm.ErrForeignKeyViolated)
}

// IsCheckConstraintViolation reports whether err is a check-constraint violation.
func IsCheckConstraintViolation(err error) bool {
	return errors.Is(err, gorm.ErrCheckConstraintViolated)
}

// IsRecordNotFound reports whether err is a no-rows-found error. Note the Table
// getters (First/Find/Last/Take) already fold not-found into a (nil, nil) result,
// so this is mainly for callers working with raw gorm results.
func IsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
