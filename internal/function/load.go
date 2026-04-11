package function

import "sevens/internal/apply"

// LoadFunction loads a function definition by name and converts it to the
// new Function type. This is a thin bridge over apply.LoadFunction +
// ConvertFunction until the EDN loading is moved out of apply entirely.
func LoadFunction(name string) (*Function, *apply.Function, error) {
	old, err := apply.LoadFunction(name)
	if err != nil {
		return nil, nil, err
	}
	return ConvertFunction(old), old, nil
}

// ListFunctions returns all available function names.
func ListFunctions() ([]string, error) {
	fns, err := apply.ListFunctions()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(fns))
	for i, f := range fns {
		names[i] = f.Name
	}
	return names, nil
}
