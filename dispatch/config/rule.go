package config

import (
	"github.com/viant/bqtail/base"
	"github.com/viant/bqtail/task"
)

//Rule represents trigger route
type Rule struct {
	When Filter `json:",omitempty"`
	task.Actions
	Info base.Info `json:",omitempty"`
}

//Init initialises rule
func (r *Rule) Init() error {
	if err := r.When.Init(); err != nil {
		return err
	}
	return nil
}
