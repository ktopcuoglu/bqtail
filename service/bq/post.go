package bq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/viant/afs/file"
	"github.com/viant/afs/url"
	"github.com/viant/bqtail/base"
	"github.com/viant/bqtail/shared"
	"github.com/viant/bqtail/task"
	"google.golang.org/api/bigquery/v2"
	"strings"
)

func (s *service) setJobID(action *task.Action) (*bigquery.JobReference, error) {
	ID := action.Meta.GetJobID()
	projectID := action.Meta.GetOrSetProject(s.Config.ProjectID)
	return &bigquery.JobReference{
		Location:  action.Meta.Region,
		JobId:     ID,
		ProjectId: projectID,
	}, nil
}

func (s *service) schedulePostTask(ctx context.Context, job *bigquery.Job, action *task.Action) error {
	if action.IsEmpty() || action.Meta.IsSyncMode() {
		return nil
	}
	action.Job = job
	data, err := json.Marshal(action)
	if err != nil {
		return errors.Wrapf(err, "failed to encode actions: %v", action)
	}
	filename := action.Meta.JobFilename()
	URL := url.Join(s.Config.AsyncTaskURL, filename)
	return base.RunWithRetries(func() error {
		return s.fs.Upload(ctx, URL, file.DefaultFileOsMode, bytes.NewReader(data))
	})
}

//Post post big query job
func (s *service) Post(ctx context.Context, callerJob *bigquery.Job, action *task.Action) (*bigquery.Job, error) {
	job, err := s.post(ctx, callerJob, action)
	if job == nil {
		job = callerJob
	} else {
		callerJob.Id = job.Id
	}
	if shared.IsDebugLoggingLevel() {
		shared.LogF("bq action: %v\n", action.Action)
		shared.LogLn(action)
	}

	if action.Meta.IsSyncMode() {
		if err == nil {
			err = base.JobError(job)
			if err == nil && !base.IsJobDone(job) {
				job, err = s.Wait(ctx, job.JobReference)
				if err == nil {
					err = base.JobError(job)
				}
			}
		}
		if shared.IsDebugLoggingLevel() && job != nil && job.Status != nil {
			shared.LogLn(job.Status)
		}
		if job == nil {
			job = callerJob
		}
		postErr := s.runActions(ctx, err, job, action.Actions)
		if postErr != nil {
			if err == nil {
				err = postErr
			} else {
				err = errors.Wrapf(err, "failed to run post action: %v", postErr)
			}
		}
	}

	if bqErr := base.JobError(job); bqErr != nil {
		errorURL := url.Join(s.ErrorURL, action.Meta.DestTable, fmt.Sprintf("%v%v", action.Meta.EventID, shared.ErrorExt))
		_ = s.fs.Upload(ctx, errorURL, file.DefaultFileOsMode, strings.NewReader(bqErr.Error()))
	}
	return job, err
}

func (s *service) post(ctx context.Context, job *bigquery.Job, action *task.Action) (*bigquery.Job, error) {
	var err error
	if job.JobReference, err = s.setJobID(action); err != nil {
		return nil, err
	}
	err = s.schedulePostTask(ctx, job, action)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to schedule bqJob %v", job.JobReference.JobId)
	}

	if shared.IsDebugLoggingLevel() {
		shared.LogLn(job)
	}
	projectID := action.Meta.GetOrSetProject(s.Config.ProjectID)
	jobService := bigquery.NewJobsService(s.Service)
	call := jobService.Insert(projectID, job)
	call.Context(ctx)
	var callJob *bigquery.Job

	err = base.RunWithRetries(func() error {
		callJob, err = call.Do()
		return err
	})

	if base.IsDuplicateJobError(err) {
		if shared.IsDebugLoggingLevel() {
			shared.LogF("duplicate job: [%v]: %v\n", job.Id, err)
		}
		err = nil
		callJob, _ = s.GetJob(ctx, job.JobReference.Location, job.JobReference.ProjectId, job.JobReference.JobId)
	}

	if err != nil {
		detail, _ := json.Marshal(job)
		err = errors.Wrapf(err, "failed to submit: %T %s", call, detail)
		if callJob == nil {
			return nil, err
		}
	}

	if err != nil || (callJob != nil && base.JobError(callJob) != nil) {
		if shared.IsDebugLoggingLevel() && callJob != nil && callJob.Status != nil {
			shared.LogLn(callJob.Status)
		}
		return callJob, err
	}
	return s.GetJob(ctx, job.JobReference.Location, job.JobReference.ProjectId, job.JobReference.JobId)
}
