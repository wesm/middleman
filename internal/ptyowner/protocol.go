package ptyowner

const (
	RequestAttach = "attach"
	RequestInput  = "input"
	RequestResize = "resize"
	RequestStatus = "status"
	RequestStop   = "stop"

	ResponseOK     = "ok"
	ResponseOutput = "output"
	ResponseExit   = "exit"
	ResponseError  = "error"
)

type Request struct {
	Type  string `json:"type"`
	Token string `json:"token,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Data  []byte `json:"data,omitempty"`
}

type Response struct {
	Type     string `json:"type"`
	OK       bool   `json:"ok,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Output   []byte `json:"output,omitempty"`
}
