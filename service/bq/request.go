package bq

import (
	"bqtail/task"
	"google.golang.org/api/bigquery/v2"
)

//Request represents base request
type Request struct {
	task.Actions
}

func (r Request) jobReference() *bigquery.JobReference {
	return &bigquery.JobReference{
		Location:  r.Region,
		JobId:     r.Info.GetJobID(),
		ProjectId: r.ProjectID,
	}
}

//PostActions returns post actions
func (r Request) PostActions() *task.Actions {
	return &r.Actions
}
