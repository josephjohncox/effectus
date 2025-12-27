package verb

func isMutatingVerbSpec(spec *Spec) bool {
	if spec == nil {
		return false
	}

	if spec.Capability&(CapWrite|CapCreate|CapDelete) != 0 {
		return true
	}

	for _, resource := range spec.Resources {
		if resource.Cap&(CapWrite|CapCreate|CapDelete) != 0 {
			return true
		}
	}

	return false
}
