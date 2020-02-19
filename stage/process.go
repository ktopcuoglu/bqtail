package stage

import (
	"github.com/viant/bqtail/shared"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"strings"
)

//Process represent an injection process
type Process struct {
	*Source        `json:",omitempty"`
	ProcessURL     string                 `json:",omitempty"`
	DoneProcessURL string                 `json:",omitempty"`
	RuleURL        string                 `json:",omitempty"`
	EventID        string                 `json:",omitempty"`
	ProjectID      string                 `json:",omitempty"`
	Region         string                 `json:",omitempty"`
	Params         map[string]interface{} `json:",omitempty"`
	Async          bool                   `json:",omitempty"`
	TempTable      string                 `json:",omitempty"`
	DestTable      string                 `json:",omitempty"`
	StepCount      int                    `json:",omitempty"`

}

func (p *Process) ActionSuffix(action string) string {
	switch action {
	case shared.ActionQuery, shared.ActionLoad, shared.ActionReload, shared.ActionCopy, shared.ActionExport:
		if p.Async {
			return shared.StepModeDispach
		}
		return shared.StepModeTail
	}
	return shared.StepModeNop

}

//IncStepCount increments and returns step count
func (p *Process) IncStepCount() int {
	p.StepCount++
	return p.StepCount
}

//Expand expand any data type
func (p Process) Expander(loadURIs []string) data.Map {
	aMap := data.Map(p.AsMap())
	aMap[shared.JobSourceKey] = p.TempTable
	aMap[shared.URLsKey] = strings.Join(loadURIs, ",")
	aMap[shared.LoadURIsKey] = loadURIs
	return aMap
}

//AsMap returns info map
func (i Process) AsMap() map[string]interface{} {
	aMap := map[string]interface{}{}
	_ = toolbox.DefaultConverter.AssignConverted(&aMap, i)
	if len(i.Params) == 0 {
		return aMap
	}
	for k, v := range i.Params {
		aMap[k] = v
	}
	return aMap
}

//GetOrSetProject initialises project ID
func (p *Process) GetOrSetProject(projectID string) string {
	if p.ProjectID != "" {
		projectID = p.ProjectID
	} else {
		p.ProjectID = projectID
	}
	return projectID
}

func (p *Process) IsSyncMode() bool {
	return ! p.Async
}


//NewProcess creates a new process
func NewProcess(eventID string, source *Source, ruleURL string, async bool) *Process {
	return &Process{
		EventID: eventID,
		RuleURL: ruleURL,
		Source:  source,
		Async:   async,
	}
}
