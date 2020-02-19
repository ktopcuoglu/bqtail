package tail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/viant/afs"
	"github.com/viant/afs/cache"
	"github.com/viant/afs/file"
	"github.com/viant/afs/option"
	astorage "github.com/viant/afs/storage"
	"github.com/viant/afs/url"
	"github.com/viant/afsc/gs"
	"github.com/viant/bqtail/base"
	"github.com/viant/bqtail/base/job"
	"github.com/viant/bqtail/service/bq"
	"github.com/viant/bqtail/service/http"
	"github.com/viant/bqtail/service/secret"
	"github.com/viant/bqtail/service/slack"
	"github.com/viant/bqtail/service/storage"
	"github.com/viant/bqtail/shared"
	"github.com/viant/bqtail/stage"
	"github.com/viant/bqtail/stage/activity"
	"github.com/viant/bqtail/stage/load"
	"github.com/viant/bqtail/tail/batch"
	"github.com/viant/bqtail/tail/config"
	"github.com/viant/bqtail/tail/contract"
	"github.com/viant/bqtail/tail/status"
	"github.com/viant/bqtail/task"
	"github.com/viant/toolbox"
	"google.golang.org/api/bigquery/v2"
	"io/ioutil"
	"strings"
	"time"
)

//Service represents a tail service
type Service interface {
	//Tails appends data from source SourceURL to matched BigQuery table
	Tail(ctx context.Context, request *contract.Request) *contract.Response
}

type service struct {
	task.Registry
	bq     bq.Service
	batch  batch.Service
	fs     afs.Service
	cfs    afs.Service
	config *Config
}

func (s *service) Init(ctx context.Context) error {
	err := s.config.Init(ctx, s.cfs)
	if err != nil {
		return err
	}
	slackService := slack.New(s.config.Region, s.config.ProjectID, s.fs, secret.New(), s.config.SlackCredentials)
	slack.InitRegistry(s.Registry, slackService)
	bqService, err := bigquery.NewService(ctx)
	if err != nil {
		return err
	}
	s.bq = bq.New(bqService, s.Registry, s.config.ProjectID, s.fs, s.config.Config)
	s.batch = batch.New(s.fs)
	bq.InitRegistry(s.Registry, s.bq)
	http.InitRegistry(s.Registry, http.New())
	storage.InitRegistry(s.Registry, storage.New(s.fs))
	return err
}

func (s *service) OnDone(ctx context.Context, request *contract.Request, response *contract.Response) {
	response.ListOpCount = gs.GetListCounter(true)
	response.StorageRetries = gs.GetRetryCodes(true)
	response.SetTimeTaken(response.Started)

	if response.Error != "" {
		errorCounterURL := url.Join(s.config.JournalURL, shared.RetryCounterSubpath, request.EventID+shared.CounterExt)
		counter, err := s.getCounterAndIncrease(ctx, errorCounterURL)
		if err != nil {
			response.CounterError = err.Error()
		}
		if counter > s.config.MaxRetries {
			response.RetryError = response.Error
			response.Status = shared.StatusOK
			response.Error = ""
			location := url.Path(request.SourceURL)
			retryDataURL := url.Join(s.config.JournalURL, shared.RetryDataSubpath, request.EventID, location)
			if err := s.fs.Move(ctx, request.SourceURL, retryDataURL); err != nil {
				response.MoveError = err.Error()
			}
			return
		}
		//Add extra sleep otherwise retry may kick in immediately and service may no be back on
		time.Sleep(3 * time.Second)
	}

	if response.Retriable {
		response.RetryError = response.Error
		return
	}

	if e := s.fs.Delete(ctx, request.SourceURL, option.NewObjectKind(true)); e != nil && response.NotFoundError == "" {
		response.NotFoundError = fmt.Sprintf("failed to delete: %v, %v", request.SourceURL, e)
	}
}

func (s *service) getCounterAndIncrease(ctx context.Context, URL string) (int, error) {
	ok, _ := s.fs.Exists(ctx, URL, option.NewObjectKind(true))
	counter := 0
	if ok {
		reader, err := s.fs.DownloadWithURL(ctx, URL)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to download counter :%v", URL)
		}
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to read counter :%v", URL)
		}
		counter = toolbox.AsInt(string(data))

	}
	counter++
	err := s.fs.Upload(ctx, URL, file.DefaultFileOsMode, strings.NewReader(fmt.Sprintf("%v", counter)))
	if err != nil {
		return counter, errors.Wrapf(err, "failed to update counter: %v", URL)
	}
	return counter, nil
}

func (s *service) Tail(ctx context.Context, request *contract.Request) *contract.Response {
	response := contract.NewResponse(request.EventID)
	response.TriggerURL = request.SourceURL

	defer s.OnDone(ctx, request, response)
	var err error
	if request.HasURLPrefix(s.config.LoadProcessPrefix) {
		err = s.runLoadProcess(ctx, request, response)
	} else if request.HasURLPrefix(s.config.PostJobPrefix) {
		err = s.runPostLoadActions(ctx, request, response)
	} else if request.HasURLPrefix(s.config.BatchPrefix) {
		err = s.runBatch(ctx, request, response)

	} else {
		err = s.tail(ctx, request, response)
	}

	if err != nil {
		response.SetIfError(err)
		if !response.Retriable {
			err = s.handlerProcessError(ctx, err, request, response)
		}
		//if storage event is duplicated, you some asset being already removed, that said do not clear table no found error
		if base.IsNotFoundError(err) && !strings.Contains(err.Error(), base.TableFragment) {
			response.NotFoundError = err.Error()
			err = nil
		}
	}
	return response
}

func (s *service) tail(ctx context.Context, request *contract.Request, response *contract.Response) error {
	response.Retriable = true
	if err := s.config.ReloadIfNeeded(ctx, s.cfs); err != nil {
		return err
	}
	rule := s.matchSourceWithRule(response, request)
	if rule == nil {
		return nil
	}
	source, err := s.fs.Object(ctx, request.SourceURL, option.NewObjectKind(true))
	if err != nil {
		response.NotFoundError = err.Error()
		response.Status = shared.StatusNotFound
		return nil
	}
	process, err := s.newProcess(ctx, source, rule, request, response)
	if err != nil {
		return err
	}
	var job *load.Job
	if rule.Batch != nil {
		job, err = s.tailInBatch(ctx, process, rule, response)
	} else {
		_, err = s.tailIndividually(ctx, process, rule, response)
		return err
	}
	if err == nil || job == nil {
		return err
	}
	return s.tryRecoverAndReport(ctx, job, response)
}

func (s *service) newProcess(ctx context.Context, source astorage.Object, rule *config.Rule, request *contract.Request, response *contract.Response) (*stage.Process, error) {
	result := stage.NewProcess(request.EventID, stage.NewSource(source.URL(), source.ModTime()), rule.Info.URL, rule.Async)
	var err error
	if result.DestTable, err = rule.Dest.ExpandTable(rule.Dest.Table, result.Source); err != nil {
		return nil, errors.Wrapf(err, "failed to expand table :%v", rule.Dest.Table)
	}
	result.ProcessURL = s.config.BuildLoadURL(result)
	result.DoneProcessURL = s.config.DoneLoadURL(result)
	result.ProjectID = s.selectProjectID(ctx, rule, response)
	result.Params, err = rule.Dest.Params(result.SourceURL)
	if base.IsLoggingEnabled() {
		fmt.Printf("process: ")
		base.Log(result)
	}
	return result, err
}

func (s *service) submitJob(ctx context.Context, job *load.Job, response *contract.Response) (*load.Job, error) {
	if len(job.Load.SourceUris) == 0 {
		return nil, errors.Errorf("sourceUris was empty")
	}
	loadRequest, action := job.NewLoadRequest()
	if err := job.Persist(ctx, s.fs); err != nil {
		response.UploadError = err.Error()
	}
	if base.IsLoggingEnabled() {
		fmt.Printf("loadRequest: ")
		base.Log(loadRequest)
	}
	bqJob, err := s.bq.Load(ctx, loadRequest, action)
	if bqJob != nil {
		job.JobStatus = bqJob.Status
		job.Statistics = bqJob.Statistics
		response.JobRef = bqJob.JobReference
		if err == nil {
			err = base.JobError(bqJob)
		}
	}
	return job, err
}

//runLoadProcess this method allows rerun Activity/Done job as long original data files are present
func (s *service) runLoadProcess(ctx context.Context, request *contract.Request, response *contract.Response) error {
	actions := []*task.Action{}
	reader, err := s.fs.DownloadWithURL(ctx, request.SourceURL)
	if err != nil {
		return err
	}
	defer func() {
		_ = reader.Close()
	}()

	if err = json.NewDecoder(reader).Decode(&actions); err != nil {
		return errors.Wrapf(err, "unable decode load action: %v", request.SourceURL)
	}
	replacement := buildJobIDReplacementMap(request.EventID, actions)

	for i, action := range actions {
		actions[i].Request = toolbox.ReplaceMapKeys(action.Request, replacement, true)
	}
	_, err = task.RunAll(ctx, s.Registry, actions)
	_, sourcePath := url.Base(request.SourceURL, "")
	journalURL := url.Join(s.config.JournalURL, sourcePath)
	if e := s.fs.Move(ctx, request.SourceURL, journalURL, option.NewObjectKind(true)); e != nil {
		response.NotFoundError = e.Error()
	}
	return err
}

func (s *service) tailIndividually(ctx context.Context, process *stage.Process, rule *config.Rule, response *contract.Response) (*load.Job, error) {
	job, err := load.NewJob(rule, process, nil)
	if err != nil {
		return nil, err
	}
	err = job.Init(ctx, s.bq)
	if err != nil {
		return nil, err
	}
	return s.submitJob(ctx, job, response)
}

func (s *service) selectProjectID(ctx context.Context, rule *config.Rule, response *contract.Response) string {
	projectID := s.config.ProjectID
	if rule.Dest.Transient != nil {
		projectPerformance, err := LoadProjectPerformance(ctx, s.fs, &s.config.Config)
		if err != nil {
			response.DownloadError = err.Error()
		}
		projectID = rule.Dest.Transient.JobProjectID(projectPerformance)
	}
	return projectID
}

func (s *service) tailInBatch(ctx context.Context, process *stage.Process, rule *config.Rule, response *contract.Response) (*load.Job, error) {
	batchWindow, err := s.batch.TryAcquireWindow(ctx, process, rule)
	if batchWindow == nil || err != nil {
		if err != nil {
			return nil, errors.Wrapf(err, "failed to acquire batch window")
		}
	}
	if batchWindow.OwnerEventID != "" {
		response.BatchingEventID = batchWindow.OwnerEventID
	}
	if batchWindow.Window == nil {
		return nil, nil
	}
	response.Window = batchWindow.Window
	if rule.Async {
		return nil, nil
	}
	return s.runInBatch(ctx, rule, batchWindow.Window, response)
}

func (s *service) runPostLoadActions(ctx context.Context, request *contract.Request, response *contract.Response) error {
	action, err := task.NewActionFromURL(ctx, s.fs, request.SourceURL)
	if err != nil {
		object, _ := s.fs.Object(ctx, request.SourceURL, option.NewObjectKind(true))
		if object == nil {
			response.NotFoundError = err.Error()
			return nil
		}
		return err
	}


	if base.IsLoggingEnabled() {
		base.Log(action)
	}
	response.Process = &action.Meta.Process
	action.Meta.Region = action.Job.JobReference.Location
	action.Meta.ProjectID = action.Job.JobReference.ProjectId
	projectID := action.Meta.GetOrSetProject(s.config.ProjectID)
	bqJob, err := s.bq.GetJob(ctx, action.Job.JobReference.Location, projectID, action.Job.JobReference.JobId)
	if err != nil {
		response.Retriable = base.IsRetryError(err)
		return errors.Wrapf(err, "failed to fetch aJob %v,", action.Job.JobReference.JobId)
	}
	if err := s.logJobInfo(ctx, bqJob); err != nil {
		response.UploadError = fmt.Sprintf("failed to log aJob info: %v", err.Error())
	}
	bqJobError := base.JobError(bqJob)
	if bqJobError != nil && bqJob.Configuration != nil && bqJob.Configuration.Load != nil {
		rule := s.config.Get(ctx, action.Meta.RuleURL)
		processJob, err := load.NewJobFromURL(ctx, rule, &action.Meta.Process, s.fs)
		if err != nil {
			return bqJobError
		}
		return s.tryRecoverAndReport(ctx, processJob, response)
	}

	if base.IsRetryError(bqJobError) {
		response.Retriable = true
		return bqJobError
	}

	if err := action.Init(ctx, s.cfs); err != nil {
		return err
	}
	bqjob := base.Job(*bqJob)
	toRun := action.ToRun(bqJobError, &bqjob)
	retriable, err := task.RunAll(ctx, s.Registry, toRun)
	if retriable {
		response.Retriable = true
	}
	if err != nil {
		return err
	}
	return bqJobError
}

func (s *service) runBatch(ctx context.Context, request *contract.Request, response *contract.Response) error {
	window, err := batch.GetWindow(ctx, request.SourceURL, s.fs)
	if err != nil {
		object, _ := s.fs.Object(ctx, request.SourceURL, option.NewObjectKind(true))
		if object == nil {
			response.NotFoundError = err.Error()
			return nil
		}
		if object.Size() == 0 {
			return err
		}
		if window, err = batch.GetWindow(ctx, request.SourceURL, s.fs); err != nil {
			return err
		}
	}
	request.EventID = window.EventID
	rule := s.config.Get(ctx, window.RuleURL)
	loadJob, batchErr := s.runInBatch(ctx, rule, window, response)
	if batchErr == nil || loadJob == nil {
		if batchErr != nil {
			response.Retriable = base.IsRetryError(batchErr)
		}
		return err
	}
	return s.tryRecoverAndReport(ctx, loadJob, response)
}

func (s *service) runInBatch(ctx context.Context, rule *config.Rule, window *batch.Window, response *contract.Response) (*load.Job, error) {
	response.Window = window
	response.BatchRunner = true
	if rule == nil {
		return nil, fmt.Errorf("rule was empty for %v", window.RuleURL)
	}
	batchingDistributionDelay := time.Duration(getRandom(shared.StorageListVisibilityDelay, rule.Batch.MaxDelayMs(shared.StorageListVisibilityDelay))) * time.Millisecond
	remainingDuration := window.End.Sub(time.Now()) + batchingDistributionDelay
	if remainingDuration > 0 {
		time.Sleep(remainingDuration)
	}
	err := s.batch.MatchWindowDataURLs(ctx, rule, window)
	if err != nil || len(window.URIs) == 0 {
		return nil, err
	}
	loadJob, jobErr := load.NewJob(rule, window.Process, window)
	if jobErr != nil {
		return nil, jobErr
	}
	err = loadJob.Init(ctx, s.bq)
	if err != nil {
		return nil, err
	}
	loadJob, err = s.submitJob(ctx, loadJob, response)
	return loadJob, err
}

func (s *service) tryRecoverAndReport(ctx context.Context, job *load.Job, response *contract.Response) error {
	err := base.JobError(job.BqJob)
	if err == nil {
		return err
	}
	//TODO report error
	return s.tryRecover(ctx, job, response)
}

func (s *service) tryRecover(ctx context.Context, job *load.Job, response *contract.Response) error {
	configuration := job.BqJob.Configuration
	response.Process = job.Process
	if configuration.Load == nil || len(configuration.Load.SourceUris) == 0 {
		err := base.JobError(job.BqJob)
		response.Retriable = base.IsRetryError(err)
		return err
	}
	response.LoadError = base.JobError(job.BqJob).Error()
	uris := status.NewURIs()
	uris.Classify(ctx, s.fs, job.BqJob)
	corruptedFileURL, invalidSchemaURL := s.getDataErrorsURLs(job.Rule)
	if err := s.moveAssets(ctx, uris.Corrupted, corruptedFileURL); err != nil {
		err = errors.Wrapf(err, "failed to move %v to %v", response.Corrupted, corruptedFileURL)
		response.MoveError = err.Error()
	}
	if err := s.moveAssets(ctx, uris.InvalidSchema, invalidSchemaURL); err != nil {
		err = errors.Wrapf(err, "failed to move %v to %v", response.InvalidSchema, invalidSchemaURL)
		response.MoveError = err.Error()
	}

	if len(uris.Valid) == 0 {
		response.Retriable = false
		return nil
	}
	response.Status = shared.StatusOK
	response.Error = ""

	job.Load.SourceUris = uris.Valid
	loadRequest, action := job.NewLoadRequest()
	meta := activity.Parse(job.BqJob.JobReference.JobId)
	action.Meta.Step = meta.Step + 1
	action.Meta = action.Meta.Wrap(shared.ActionReload)
	loadJob, err := s.bq.Load(ctx, loadRequest, action)
	if err == nil {
		err = base.JobError(loadJob)
	}
	return err
}

func (s *service) getDataErrorsURLs(rule *config.Rule) (string, string) {
	corruptedFileURL := s.config.CorruptedFileURL
	invalidSchemaURL := s.config.InvalidSchemaURL
	if rule != nil {
		if rule.CorruptedFileURL != "" {
			corruptedFileURL = rule.CorruptedFileURL
		}
		if rule.InvalidSchemaURL != "" {
			invalidSchemaURL = rule.InvalidSchemaURL
		}
	}
	return corruptedFileURL, invalidSchemaURL
}

func (s *service) moveAssets(ctx context.Context, URLs []string, baseDestURL string) error {
	var err error
	if len(URLs) == 0 {
		return nil
	}
	for _, sourceURL := range URLs {
		_, URLPath := url.Base(sourceURL, "")
		destURL := url.Join(baseDestURL, URLPath)
		e := s.fs.Move(ctx, sourceURL, destURL)

		if base.IsLoggingEnabled() {
			base.Log(fmt.Sprintf("moving: %v %v, %v\n", sourceURL, destURL, err))
		}
		if e != nil {
			if exists, _ := s.fs.Exists(ctx, sourceURL, option.NewObjectKind(true)); !exists {
				continue
			}
			err = e
		}
	}
	return err
}

func (s *service) handlerProcessError(ctx context.Context, err error, request *contract.Request, response *contract.Response) error {
	info := response.Process
	if info == nil || err == nil {
		return err
	}
	activeURL := s.config.BuildLoadURL(info)

	//Replay the whole load process - some individual BigQuery job can not be recovered
	if base.IsInternalError(err) || base.IsBackendError(err) {
		if exists, _ := s.fs.Exists(ctx, activeURL); exists {
			return s.replayLoadProcess(ctx, activeURL, request)
		}
	}
	response.SetIfError(err)
	if data, e := json.Marshal(response); e == nil {
		errorResponseURL := url.Join(s.config.ErrorURL, info.DestTable, fmt.Sprintf("%v%v", request.EventID, shared.ResponseErrorExt))
		if e := s.fs.Upload(ctx, errorResponseURL, file.DefaultFileOsMode, bytes.NewReader(data)); e != nil {
			response.UploadError = e.Error()
		}

	}
	errorURL := url.Join(s.config.ErrorURL, info.DestTable, fmt.Sprintf("%v%v", request.EventID, shared.ErrorExt))
	if e := s.fs.Upload(ctx, errorURL, file.DefaultFileOsMode, strings.NewReader(err.Error())); e != nil {
		response.UploadError = e.Error()
	}
	processErrorURL := url.Join(s.config.ErrorURL, info.DestTable, fmt.Sprintf("%v%v", request.EventID, shared.ProcessExt))
	_ = s.fs.Copy(ctx, activeURL, processErrorURL)
	doneURL := s.config.DoneLoadURL(info)
	_ = s.fs.Move(ctx, activeURL, doneURL)
	return err
}

func (s *service) replayLoadProcess(ctx context.Context, sourceURL string, request *contract.Request) error {
	bucket := url.Host(request.SourceURL)
	_, name := url.Split(sourceURL, gs.Scheme)
	loadJobURL := fmt.Sprintf("gs://%v/%v/%v", bucket, s.config.LoadProcessPrefix, name)
	return s.fs.Copy(ctx, sourceURL, loadJobURL)
}

func (s *service) logJobInfo(ctx context.Context, bqjob *bigquery.Job) error {
	if s.config.BqJobInfoPath == "" {
		return nil
	}
	info := job.NewInfo(bqjob)
	URL := url.Join(fmt.Sprintf("gs://%v/", s.config.TriggerBucket), s.config.BqJobInfoPath, bqjob.JobReference.JobId+shared.JSONExt)
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return s.fs.Upload(ctx, URL, file.DefaultFileOsMode, bytes.NewReader(data))
}

//New creates a new service
func New(ctx context.Context, config *Config) (Service, error) {
	srv := &service{
		config:   config,
		fs:       afs.New(),
		cfs:      cache.Singleton(config.URL),
		Registry: task.NewRegistry(),
	}
	return srv, srv.Init(ctx)
}
