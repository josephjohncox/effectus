package types

// FunctionSpec describes an expression function signature.
type FunctionSpec struct {
	Name        string
	Func        interface{}
	ArgTypes    []string
	ReturnType  string
	Unsafe      bool
	Description string
}

// RegisterFunctionSpec registers a function signature for expression type checking.
func (ts *TypeSystem) RegisterFunctionSpec(spec *FunctionSpec) {
	if spec == nil {
		return
	}
	if ts.functions == nil {
		ts.functions = make(map[string]*FunctionSpec)
	}
	ts.functions[spec.Name] = spec
}

// GetFunctionSpec returns the function spec if registered.
func (ts *TypeSystem) GetFunctionSpec(name string) (*FunctionSpec, bool) {
	if ts.functions == nil {
		return nil, false
	}
	spec, ok := ts.functions[name]
	return spec, ok
}

// GetFunctionSpecs returns all function specs.
func (ts *TypeSystem) GetFunctionSpecs() map[string]*FunctionSpec {
	result := make(map[string]*FunctionSpec)
	for name, spec := range ts.functions {
		result[name] = spec
	}
	return result
}
