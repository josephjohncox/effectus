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
	// TransactionProgramTag marks a saga transaction boundary
	TransactionProgramTag
)

// Program represents a free monad over Effect with saga transaction support
type Program struct {
	Tag         ProgramTag
	Pure        interface{}
	Effect      effectus.Effect
	Transaction *TransactionInfo
	Continue    func(interface{}) *Program
}

// TransactionInfo holds metadata for saga transaction boundaries
type TransactionInfo struct {
	SagaID       string   // Unique saga identifier
	Name         string   // Human-readable transaction name
	Compensation string   // Inverse verb for compensation
	Program      *Program // The program to execute within the transaction
	IsAtomic     bool     // Whether this transaction should be atomic
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

// DoWithCompensation creates a program that performs an effect with compensation info
func DoWithCompensation(effect effectus.Effect, compensation string, cont func(interface{}) *Program) *Program {
	// Enhance the effect with compensation metadata
	enhancedEffect := effect
	if payload, ok := effect.Payload.(map[string]interface{}); ok {
		enhancedPayload := make(map[string]interface{})
		for k, v := range payload {
			enhancedPayload[k] = v
		}
		enhancedPayload["_compensation"] = compensation
		enhancedEffect.Payload = enhancedPayload
	}

	return &Program{
		Tag:      EffectProgramTag,
		Effect:   enhancedEffect,
		Continue: cont,
	}
}

// Transaction creates a saga transaction boundary around a program
func Transaction(sagaID, name string, program *Program) *Program {
	return &Program{
		Tag: TransactionProgramTag,
		Transaction: &TransactionInfo{
			SagaID:   sagaID,
			Name:     name,
			Program:  program,
			IsAtomic: true,
		},
		Continue: func(_ interface{}) *Program {
			return Pure(nil) // Transaction completion
		},
	}
}

// Atomic creates an atomic transaction around a program
func Atomic(name string, program *Program) *Program {
	return Transaction("", name, program) // SagaID will be generated at runtime
}

// Bind sequences two programs together (monadic bind)
func (p *Program) Bind(f func(interface{}) *Program) *Program {
	if p.Tag == PureProgramTag {
		return f(p.Pure)
	}

	if p.Tag == TransactionProgramTag {
		// For transactions, bind applies to the inner program
		return &Program{
			Tag: TransactionProgramTag,
			Transaction: &TransactionInfo{
				SagaID:   p.Transaction.SagaID,
				Name:     p.Transaction.Name,
				Program:  p.Transaction.Program.Bind(f),
				IsAtomic: p.Transaction.IsAtomic,
			},
			Continue: p.Continue,
		}
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

// WithCompensation adds compensation information to all effects in the program
func (p *Program) WithCompensation(getCompensation func(verb string) string) *Program {
	if p.Tag == PureProgramTag {
		return p
	}

	if p.Tag == TransactionProgramTag {
		// Apply compensation to the inner program
		return &Program{
			Tag: TransactionProgramTag,
			Transaction: &TransactionInfo{
				SagaID:   p.Transaction.SagaID,
				Name:     p.Transaction.Name,
				Program:  p.Transaction.Program.WithCompensation(getCompensation),
				IsAtomic: p.Transaction.IsAtomic,
			},
			Continue: p.Continue,
		}
	}

	// For effects, add compensation metadata
	compensation := getCompensation(p.Effect.Verb)
	return &Program{
		Tag:    EffectProgramTag,
		Effect: p.addCompensationToEffect(compensation),
		Continue: func(result interface{}) *Program {
			return p.Continue(result).WithCompensation(getCompensation)
		},
	}
}

// addCompensationToEffect adds compensation metadata to an effect
func (p *Program) addCompensationToEffect(compensation string) effectus.Effect {
	if compensation == "" {
		return p.Effect
	}

	enhanced := p.Effect
	if payload, ok := p.Effect.Payload.(map[string]interface{}); ok {
		enhancedPayload := make(map[string]interface{})
		for k, v := range payload {
			enhancedPayload[k] = v
		}
		enhancedPayload["_compensation"] = compensation
		enhanced.Payload = enhancedPayload
	}
	return enhanced
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

// FromListWithCompensation converts effects to a program with compensation
func FromListWithCompensation(effects []effectus.Effect, compensations map[string]string) *Program {
	program := FromList(effects)
	return program.WithCompensation(func(verb string) string {
		return compensations[verb]
	})
}

// ToTransaction wraps a program in a saga transaction
func (p *Program) ToTransaction(sagaID, name string) *Program {
	return Transaction(sagaID, name, p)
}

// ToAtomic wraps a program in an atomic transaction
func (p *Program) ToAtomic(name string) *Program {
	return Atomic(name, p)
}

// Run executes a program with an executor, returning the final value
// This preserves the simple, elegant execution model
func Run(program *Program, executor effectus.Executor) (interface{}, error) {
	if program.Tag == PureProgramTag {
		// Check if the Pure value is an error
		if err, isError := program.Pure.(error); isError {
			return nil, err
		}
		return program.Pure, nil
	}

	if program.Tag == TransactionProgramTag {
		// For transactions, execute the inner program
		// The saga handling is done at a higher level (in the executors)
		return Run(program.Transaction.Program, executor)
	}

	result, err := executor.Do(program.Effect)
	if err != nil {
		return nil, err
	}

	next := program.Continue(result)
	return Run(next, executor)
}

// ExtractTransactions extracts all transaction boundaries from a program
// This is useful for saga executors to understand the transaction structure
func ExtractTransactions(program *Program) []*TransactionInfo {
	var transactions []*TransactionInfo

	var extract func(*Program)
	extract = func(p *Program) {
		if p == nil {
			return
		}

		if p.Tag == TransactionProgramTag {
			transactions = append(transactions, p.Transaction)
			extract(p.Transaction.Program) // Recurse into nested transactions
			return
		}

		if p.Tag == EffectProgramTag {
			// For effects, we can't statically determine the continuation
			// This is a limitation of the free monad approach for static analysis
			// The saga executor will handle this dynamically
			return
		}
	}

	extract(program)
	return transactions
}

// IsTransactional returns true if the program contains any transaction boundaries
func (p *Program) IsTransactional() bool {
	transactions := ExtractTransactions(p)
	return len(transactions) > 0
}

// GetCompensation extracts compensation information from an effect
func GetCompensation(effect effectus.Effect) string {
	if payload, ok := effect.Payload.(map[string]interface{}); ok {
		if comp, exists := payload["_compensation"]; exists {
			if compStr, ok := comp.(string); ok {
				return compStr
			}
		}
	}
	return ""
}
