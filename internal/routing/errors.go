package routing

import "fmt"

type RouteError struct {
	Code    string
	Message string
}

func (e *RouteError) Error() string { return e.Message }

func errMissingModel() *RouteError {
	return &RouteError{Code: "missing_model", Message: "model is required"}
}

func errUnknownModel(m string) *RouteError {
	return &RouteError{Code: "unknown_model", Message: fmt.Sprintf("unknown model %q", m)}
}

// IsUnknownModel reports a missing or invalid default-group route.
func IsUnknownModel(err error) bool {
	if re, ok := err.(*RouteError); ok {
		return re.Code == "unknown_model"
	}
	return false
}

// IsMissingModel reports empty model errors.
func IsMissingModel(err error) bool {
	if re, ok := err.(*RouteError); ok {
		return re.Code == "missing_model"
	}
	return false
}
