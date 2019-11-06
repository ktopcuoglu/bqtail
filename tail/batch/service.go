package batch

import (
	"bqtail/tail/config"
	"bqtail/tail/contract"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/viant/afs"
	"github.com/viant/afs/file"
	"github.com/viant/afs/matcher"
	"github.com/viant/afs/storage"
	"github.com/viant/afs/url"
	"io/ioutil"
	"log"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service interface {
	//Add adds transfer events to batch stage
	Add(ctx context.Context, sourceCreated time.Time, request *contract.Request, route *config.Rule) error

	//Try to acquire batch window
	TryAcquireWindow(ctx context.Context, request *contract.Request, route *config.Rule) (*BatchedWindow, error)

	//MatchWindowData updates the window with the window span matched transfer datafiles
	MatchWindowData(ctx context.Context, now time.Time, window *Window, route *config.Rule) error
}

type service struct {
	URL string
	afs.Service
}

func (s *service) scheduleURL(created time.Time, request *contract.Request, route *config.Rule) (string, error) {
	dest, err := route.Dest.ExpandTable(created, request.SourceURL)
	if err != nil {
		return "", err
	}
	baseURL := url.Join(s.URL, path.Join(dest))
	return url.Join(baseURL, request.EventID+transferableExtension), nil
}

//Add adds matched transfer event to batch stage
func (s *service) Add(ctx context.Context, sourceCreated time.Time, request *contract.Request, route *config.Rule) error {
	URL, err := s.scheduleURL(sourceCreated, request, route)
	if err != nil {
		return err
	}
	if err = s.Upload(ctx, URL, file.DefaultFileOsMode, strings.NewReader(request.SourceURL)); err != nil {
		return err
	}
	return nil
}

func (s *service) AcquireWindow(ctx context.Context, baseURL string, window *Window) error {
	URL := url.Join(baseURL, fmt.Sprintf("%v%v", window.End.UnixNano(), windowExtension))
	data, err := json.Marshal(window)
	if err != nil {
		return err
	}
	err = s.Upload(ctx, URL, file.DefaultFileOsMode, bytes.NewReader(data))
	return err
}

func (s *service) getSchedule(ctx context.Context, created time.Time, request *contract.Request, route *config.Rule) (storage.Object, error) {
	URL, err := s.scheduleURL(created, request, route)
	if err != nil {
		return nil, err
	}
	return s.Object(ctx, URL)
}

func (s *service) getWindow(ctx context.Context, URL string) (*Window, error) {
	reader, err := s.DownloadWithURL(ctx, URL)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read window: %v", URL)
	}
	window := &Window{}
	return window, json.Unmarshal(data, window)
}

func (s *service) getBatchingWindowID(ctx context.Context, sourceTime time.Time, windows []storage.Object) (string, error) {
	for i := range windows {
		windowEnd, err := windowToTime(windows[i])
		if err != nil {
			return "", err
		}
		if sourceTime.After(*windowEnd) {
			continue
		}
		window, err := s.getWindow(ctx, windows[0].URL())
		if err != nil {
			return "", err
		}
		if !windowEnd.Equal(window.End) {
			log.Printf("invalid window end tiem %v %v %v %v\n", window.EventID, window.Start, window.End, windowEnd)
		}
		if sourceTime.Before(window.Start) ||sourceTime.After(window.End)  {
			continue
		}
		return window.EventID, nil
	}
	return "", nil
}

//TryAcquireWindow try to acquire window for batched transfer, only one cloud function can acquire window
func (s *service) TryAcquireWindow(ctx context.Context, request *contract.Request, route *config.Rule) (*BatchedWindow, error) {
	source, err := s.Object(ctx, request.SourceURL)
	if err != nil {
		return nil, errors.Wrapf(err, "source event was missing: %v", request.SourceURL)
	}
	dest, err := route.Dest.ExpandTable(source.ModTime(), request.SourceURL)
	if err != nil {
		return nil, err
	}
	eventSchedule, err := s.getSchedule(ctx, source.ModTime(), request, route)
	if err != nil {
		return nil, err
	}
	baseURL := url.Join(s.URL, path.Join(dest))
	windowMin := eventSchedule.ModTime().Add(-(route.Batch.Window.Duration + 1))
	windowMax := eventSchedule.ModTime().Add(route.Batch.Window.Duration + 1)

	transferableMatcher := windowedMatcher(windowMin, windowMax, transferableExtension)
	transfers, err := s.List(ctx, baseURL, transferableMatcher.Match)
	if err != nil {
		return nil, err
	}
	if len(transfers) == 0 {
		return nil, fmt.Errorf("scheduled were empty, expected at least one")
	}
	sortedTransfers := Objects(transfers)
	sort.Sort(sortedTransfers)
	window := NewWindow(baseURL, request, eventSchedule.ModTime(), route, source.ModTime())
	before := sortedTransfers.Before(eventSchedule)
	if len(before) == 0 {
		return &BatchedWindow{Window: window}, s.AcquireWindow(ctx, baseURL, window)
	}

	windowMatcher := windowedMatcher(windowMin.Add(-route.Batch.Window.Duration), windowMax, windowExtension)
	windows, err := s.List(ctx, baseURL, windowMatcher.Match)

	batchingEventID := before[0].Name()
	if len(windows) == 0 {
		//this instance can not acquire batch when
		//- no active window, and has some earlier transfer
		//- more than 1 windows, meaning has to be acquire by other instance
		return &BatchedWindow{BatchingEventID: batchingEventID}, nil
	}

	batchingEventID, err = s.getBatchingWindowID(ctx, source.ModTime(), windows)
	if err != nil || batchingEventID != "" {
		return nil, err
	}
	return &BatchedWindow{Window: window}, s.AcquireWindow(ctx, baseURL, window)
}

func (s *service) loadDatafile(ctx context.Context, object storage.Object) (*Datafile, error) {
	reader, err := s.Download(ctx, object)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	_, name := url.Split(object.URL(), file.Scheme)
	name = string(name[:len(name)-4])
	return &Datafile{SourceURL: string(data), EventID: name, Created: object.ModTime(), URL: object.URL()}, nil
}

//MatchWindowData matches window data, it waits for window to ends if needed
func (s *service) MatchWindowData(ctx context.Context, now time.Time, window *Window, route *config.Rule) error {
	tillWindowEnd := window.End.Sub(now)
	if tillWindowEnd > 0 {
		//wait for window to end
		time.Sleep(tillWindowEnd + 1)
	}
	eventMatcher := windowedMatcher(window.Start.Add(-1), window.End.Add(1), transferableExtension)
	parentURL, _ := url.Split(window.URL, file.Scheme)
	transferFiles, err := s.List(ctx, parentURL, eventMatcher)
	if err != nil {
		return err
	}
	window.Datafiles = make([]*Datafile, 0)
	for i := range transferFiles {
		if transferFiles[i].ModTime().Before(window.Start) || transferFiles[i].ModTime().After(window.End) {
			continue
		}
		datafile, err := s.loadDatafile(ctx, transferFiles[i])
		if err != nil {
			return err
		}
		window.Datafiles = append(window.Datafiles, datafile)
	}
	return nil
}

func windowToTime(window storage.Object) (*time.Time, error) {
	name := window.Name()
	nanoTime := string(name[:len(name)-4])
	unixNano, err := strconv.ParseInt(nanoTime, 10, 64)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid nano time for URL: %v", window.URL())
	}
	result := time.Unix(0, unixNano)
	return &result, nil
}

func windowedMatcher(after, before time.Time, ext string) *matcher.Modification {
	extMatcher, _ := matcher.NewBasic("", ext, "", nil)
	modTimeMatcher := matcher.NewModification(&before, &after, extMatcher.Match)
	return modTimeMatcher
}

//New create stage service
func New(batchURL string, storageService afs.Service) Service {
	return &service{
		URL:     batchURL,
		Service: storageService,
	}
}
