package runnerproto

// Param is a simple key/value pipeline parameter, substituted into task
// command/args/env by the hub before the PipelineRun is dispatched.
type Param struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
