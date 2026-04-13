package function

// ParseOutputSignature converts an output string (from EDN or triple store)
// to a Signature. This is the single source of truth for output-string mapping.
func ParseOutputSignature(output, outputType string) Signature {
	switch output {
	case "ops":
		return Signature{Shape: ShapeFileOps, TypeName: outputType}
	case "create":
		typeName := outputType
		if typeName == "" {
			typeName = "create"
		}
		return Signature{Shape: ShapeFileOps, TypeName: typeName}
	case "edit":
		typeName := outputType
		if typeName == "" {
			typeName = "edit"
		}
		return Signature{Shape: ShapeFileOps, TypeName: typeName}
	case "suggestions":
		return Signature{Shape: ShapeStructured, TypeName: outputType}
	default:
		return Signature{Shape: ShapeText}
	}
}

// ParseInputSignature converts an input string (from EDN or triple store)
// to a Signature. This is the single source of truth for input-string mapping.
func ParseInputSignature(input string) Signature {
	switch input {
	case "ops":
		return Signature{Shape: ShapeFileOps}
	case "suggestions":
		return Signature{Shape: ShapeStructured}
	default:
		return Signature{Shape: ShapeText}
	}
}
