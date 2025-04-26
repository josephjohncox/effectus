package flow

import (
	"github.com/effectus/effectus-go"
)

// ProgramTag identifies the type of program node
type ProgramTag int

const (
	// PureProgramTag is a pure value
	PureProgramTag ProgramTag = iota
	// EffectProgramTag is an effect with a continuation
	EffectProgramTag
)

// Program represents a free monad over Effect
type Program struct {
	Tag      ProgramTag
	Pure     interface{}
	Effect   effectus.Effect
	Continue func(interface{}) *Program
}

// Pure creates a program that just returns a value
func Pure(value interface{}) *Program {
	return &Program{
		Tag:  PureProgramTag,
		Pure: value,
	}
}

// Do creates a program that performs an effect then continues
func Do(effect effectus.Effect, cont func(interface{}) *Program) *Program {
	return &Program{
		Tag:      EffectProgramTag,
		Effect:   effect,
		Continue: cont,
	}
}

// Bind sequences two programs together (monadic bind)
func (p *Program) Bind(f func(interface{}) *Program) *Program {
	if p.Tag == PureProgramTag {
		return f(p.Pure)
	}

	return &Program{
		Tag:    EffectProgramTag,
		Effect: p.Effect,
		Continue: func(result interface{}) *Program {
			return p.Continue(result).Bind(f)
		},
	}
}

// FlatMap is an alias for Bind with a more familiar name
func (p *Program) FlatMap(f func(interface{}) *Program) *Program {
	return p.Bind(f)
}

// Map transforms the result value with a pure function
func (p *Program) Map(f func(interface{}) interface{}) *Program {
	return p.Bind(func(value interface{}) *Program {
		return Pure(f(value))
	})
}

// Then chains a program after this one, ignoring this program's result
func (p *Program) Then(next *Program) *Program {
	return p.Bind(func(_ interface{}) *Program {
		return next
	})
}

// FromList converts a list of Effects to a Program
// This is the "canonical embedding" α from the theoretical foundation
func FromList(effects []effectus.Effect) *Program {
	if len(effects) == 0 {
		return Pure(nil)
	}

	head := effects[0]
	tail := effects[1:]

	return Do(head, func(_ interface{}) *Program {
		return FromList(tail)
	})
}

// Run executes a program with an executor, returning the final value
func Run(program *Program, executor effectus.Executor) (interface{}, error) {
	if program.Tag == PureProgramTag {
		return program.Pure, nil
	}

	result, err := executor.Do(program.Effect)
	if err != nil {
		return nil, err
	}

	next := program.Continue(result)
	return Run(next, executor)
}
