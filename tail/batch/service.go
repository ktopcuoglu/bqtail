package batch

import (
	"github.com/viant/bqtail/base"
	"github.com/viant/bqtail/shared"
	"github.com/viant/bqtail/stage"
	"github.com/viant/bqtail/tail/config"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/viant/afs"
	"github.com/viant/afs/file"
	"github.com/viant/afs/matcher"
	"github.com/viant/afs/option"
	"github.com/viant/afs/url"
	"github.com/viant/afsc/gs"
	"io/ioutil"
	"path"
	"strings"
)

type ProjectSelector func() string

//Service representa a batch service
type Service interface {
	//Try to acquire batch window
	TryAcquireWindow(ctx context.Context, process *stage.Process, rule *config.Rule) (*Info, error)

	//MatchWindowDataURLs returns matching data URLs
	MatchWindowDataURLs(ctx context.Context, rule *config.Rule, window *Window) error
}

type service struct {
	fs afs.Service
}

func (s *service) addLocationFile(ctx context.Context, window *Window, location string) error {
	locationFile := fmt.Sprintf("%v%v", base.Hash(location), shared.LocationExt)
	URL := strings.Replace(window.URL, shared.WindowExt, "/"+locationFile, 1)
	if ok, _ := s.fs.Exists(ctx, URL, option.NewObjectKind(true)); ok {
		return nil
	}
	return s.fs.Upload(ctx, URL, file.DefaultDirOsMode, strings.NewReader(location))
}

//TryAcquireWindow try to acquire window for batched transfer, only one cloud function can acquire window
func (s *service) TryAcquireWindow(ctx context.Context, process *stage.Process, rule *config.Rule) (*Info, error) {
	parentURL, _ := url.Split(process.Source.SourceURL, gs.Scheme)
	windowDest := process.DestTable
	if !rule.Batch.MultiPath {
		//one batch per folder location
		windowDest = fmt.Sprintf("%v_%v", process.DestTable, base.Hash(parentURL))
	}
	batch := rule.Batch
	windowURL := batch.WindowURL(windowDest, process.Source.SourceTime)
	exists, _ := s.fs.Exists(ctx, windowURL, option.NewObjectKind(true))

	endTime := batch.WindowEndTime(process.Source.SourceTime)
	startTime := endTime.Add(-batch.Window.Duration)
	var err error
	var window *Window
	if exists {
		window = NewWindow(process, startTime, endTime, windowURL)
		if rule.Batch.MultiPath {
			err = s.addLocationFile(ctx, window, parentURL)
		}
		return &Info{OwnerEventID: window.EventID}, err
	}

	if batch.RollOver && !batch.IsWithinFirstHalf(process.Source.SourceTime) {
		prevWindowURL := batch.WindowURL(process.DestTable, process.Source.SourceTime.Add(-(1 + batch.Window.Duration)))
		if exists, _ := s.fs.Exists(ctx, prevWindowURL, option.NewObjectKind(true)); !exists {
			startTime = startTime.Add(-batch.Window.Duration)
		}
	}
	window = NewWindow(process, startTime, endTime, windowURL)
	windowData, _ := json.Marshal(window)
	err = s.fs.Upload(ctx, windowURL, file.DefaultFileOsMode, bytes.NewReader(windowData), option.NewGeneration(true, 0))

	if isPreConditionError(err) || isRateError(err) {
		window := NewWindow(process, startTime, endTime, windowURL)
		if rule.Batch.MultiPath {
			if err = s.addLocationFile(ctx, window, parentURL); err != nil {
				return nil, err
			}
		}
		return &Info{OwnerEventID: window.EventID}, nil
	}
	if rule.Batch.MultiPath {
		err = s.addLocationFile(ctx, window, parentURL)
	}
	return &Info{Window: window}, err
}

func (s *service) readLocation(ctx context.Context, URL string) (string, error) {
	reader, err := s.fs.DownloadWithURL(ctx, URL)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = reader.Close()
	}()
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *service) getBaseURLS(ctx context.Context, rule *config.Rule, window *Window) ([]string, error) {
	var baseURLs = make(map[string]bool)
	baseURL, _ := url.Split(window.Source.SourceURL, gs.Scheme)
	baseURLs[baseURL] = true

	if rule.Batch.MultiPath {
		window.Locations = make([]string, 0)
		URL := strings.Replace(window.URL, shared.WindowExt, "/", 1)
		objects, err := s.fs.List(ctx, URL)
		if err != nil {
			return nil, err
		}
		for _, object := range objects {
			if object.IsDir() || path.Ext(object.Name()) != shared.LocationExt {
				continue
			}
			location, err := s.readLocation(ctx, object.URL())
			if err != nil {
				return nil, errors.Wrapf(err, "failed to load location: %v", object.URL())
			}
			window.Locations = append(window.Locations, object.URL())
			baseURLs[location] = true
		}
	}
	var result = make([]string, 0)
	for k := range baseURLs {
		result = append(result, k)
	}
	return result, nil
}

//MatchWindowData matches window data, it waits for window to ends if needed
func (s *service) MatchWindowDataURLs(ctx context.Context, rule *config.Rule, window *Window) error {
	before := window.End           //inclusive
	afeter := window.Start.Add(-1) //exclusive
	modFilter := matcher.NewModification(&before, &afeter)
	baseURLS, err := s.getBaseURLS(ctx, rule, window)
	if err != nil {
		return errors.Wrapf(err, "failed get batch location: %v", window.URL)
	}
	var result = make([]string, 0)
	for _, baseURL := range baseURLS {
		if err := s.matchData(ctx, window, rule, baseURL, modFilter, &result); err != nil {
			return err
		}
	}
	window.URIs = result
	return nil
}


func (s *service) matchData(ctx context.Context, window *Window, rule *config.Rule, baseURL string, matcher option.Matcher, result *[]string) error {
	objects, err := s.fs.List(ctx, baseURL)
	if err != nil {
		return errors.Wrapf(err, "failed to list batch %v data files", baseURL)
	}
	for _, object := range objects {
		if rule.HasMatch(object.URL()) {
			source := stage.NewSource(object.URL(), object.ModTime())
			table, err := rule.Dest.ExpandTable(rule.Dest.Table, source)
			if err != nil {
				return errors.Wrapf(err, "failed to expand table: %v", rule.Dest.Table)
			}
			if table != window.DestTable {
				continue
			}
			if object.ModTime().After(window.End) || object.ModTime().Equal(window.End) {
				continue
			}
			if object.ModTime().Before(window.Start) {
				continue
			}
			*result = append(*result, object.URL())
		}
	}
	return nil
}

//New create stage service
func New(storageService afs.Service) Service {
	return &service{
		fs: storageService,
	}
}
