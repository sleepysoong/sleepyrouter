package usage

// Record is a completed request row.
type Record struct {
	RequestID      string
	Protocol       string
	RequestedModel string
	RoutedModel    string
	Provider       string
	Attempts       int
	InputTokens    int64
	OutputTokens   int64
	Success        bool
	ErrorClass     string
	DurationMs     int64
	SessionID      string
	ConfigGen      uint64
}

// Attempt is a per-candidate row.
type Attempt struct {
	RequestID  string
	Index      int
	Model      string
	Provider   string
	DurationMs int64
	Success    bool
	StatusCode int
	ErrorClass string
}
