// Package types provides type representation and validation for Effectus
package types

import (
	"github.com/effectus/effectus-go/schema/common"
)

// Type is an alias for common.Type
type Type = common.Type

// PrimitiveType is an alias for common.PrimitiveType
type PrimitiveType = common.PrimitiveType

// Type constants
const (
	TypeUnknown  = common.TypeUnknown
	TypeBool     = common.TypeBool
	TypeInt      = common.TypeInt
	TypeFloat    = common.TypeFloat
	TypeString   = common.TypeString
	TypeList     = common.TypeList
	TypeMap      = common.TypeMap
	TypeTime     = common.TypeTime
	TypeDate     = common.TypeDate
	TypeDuration = common.TypeDuration
	TypeObject   = common.TypeObject
)

// ResolutionResult is an alias for common.ResolutionResult
type ResolutionResult = common.ResolutionResult

// TypeCheckResult is an alias for common.TypeCheckResult
type TypeCheckResult = common.TypeCheckResult

// NewTypeError delegates to common.NewTypeError
func NewTypeError(format string, args ...interface{}) error {
	return common.NewTypeError(format, args...)
}
