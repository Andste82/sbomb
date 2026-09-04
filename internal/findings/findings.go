package findings

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Finding struct {
	ID       string
	Severity Severity
	Message  string
	Detail   map[string]any
}

func (f Finding) IsWaived() bool { return false }
