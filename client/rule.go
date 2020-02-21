package client

import (
	"context"
	"github.com/pkg/errors"
	"github.com/viant/afs/file"
	"github.com/viant/afs/url"
	"github.com/viant/bqtail/shared"
	"github.com/viant/bqtail/tail/config"
	"github.com/viant/toolbox"
	"gopkg.in/yaml.v2"
	"time"
)

//BuildRuleRequest represents build rule request
type BuildRuleRequest struct {
	ProjectID string
	Bucket    string
	BasePath  string
	Window    int
	SourceURL string
}

func (s *service) BuildRule(ctx context.Context, request *BuildRuleRequest) (*config.Rule, error) {
	return nil, nil
}

func (s *service) loadRule(ctx context.Context, URL string) (*config.Rule, error) {
	reader, err := s.fs.DownloadWithURL(ctx, URL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to download rule: %v", URL)
	}
	defer reader.Close()
	_, name := url.Split(URL, "")
	ruleURL := url.Join(s.config.RulesURL, name)
	err = s.fs.Upload(ctx, ruleURL, file.DefaultFileOsMode, reader)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update rule: %v", ruleURL)
	}
	//add sleep to refresh
	time.Sleep(1 * time.Millisecond)
	err = s.config.ReloadIfNeeded(ctx, s.fs)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to reload rules: %v", ruleURL)
	}

	rule := s.config.Rule(ctx, ruleURL)
	if rule == nil {
		return nil, errors.Errorf("failed to lookup rule: %v", ruleURL)
	}
	return rule, nil
}


func (s *service) reportRule(rule *config.Rule)  {
	ruleMap := map[string]interface{}{}
	toolbox.DefaultConverter.AssignConverted(&ruleMap, rule)
	compactedMap := map[string]interface{}{}
	toolbox.CopyMap(ruleMap, compactedMap, toolbox.OmitEmptyMapWriter)
	var ruleYAML, err = yaml.Marshal(compactedMap)
	if err == nil {
		shared.LogF("==== USING RULE ===\n%s\n", ruleYAML)
	}
}

