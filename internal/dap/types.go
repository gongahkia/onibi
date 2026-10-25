package dap

type Source struct {
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	Reference int    `json:"sourceReference,omitempty"`
}
type SourceBreakpoint struct {
	Line         int    `json:"line"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
	LogMessage   string `json:"logMessage,omitempty"`
}
type Breakpoint struct {
	ID       int    `json:"id"`
	Verified bool   `json:"verified"`
	Message  string `json:"message"`
	Source   Source `json:"source"`
	Line     int    `json:"line"`
}
type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type StackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}
type Scope struct {
	Name      string `json:"name"`
	Reference int    `json:"variablesReference"`
	Expensive bool   `json:"expensive"`
	Named     int    `json:"namedVariables"`
	Indexed   int    `json:"indexedVariables"`
}
type Variable struct {
	Name         string `json:"name"`
	Value        string `json:"value"`
	Type         string `json:"type"`
	EvaluateName string `json:"evaluateName"`
	Reference    int    `json:"variablesReference"`
	Named        int    `json:"namedVariables"`
	Indexed      int    `json:"indexedVariables"`
}
type Stopped struct {
	Reason      string `json:"reason"`
	Description string `json:"description"`
	ThreadID    int    `json:"threadId"`
	AllThreads  bool   `json:"allThreadsStopped"`
}
