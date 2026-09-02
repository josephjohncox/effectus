// Package embedded preserves the v0.3 embedded API during migration.
package embedded

import root "github.com/josephjohncox/effectus/embedded"

type HandlerFunc = root.HandlerFunc
type Resource = root.Resource
type Verb = root.Verb
type Request = root.Request
type Builder = root.Builder
type Runtime = root.Runtime

var New = root.New
var Success = root.Success
var Retryable = root.Retryable
var Permanent = root.Permanent
var Unknown = root.Unknown
