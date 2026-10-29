package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

var viewSourceLocks sync.Map

type viewSourceLock struct {
	mu        sync.Mutex
	completed time.Time
}

func (s *Server) resolveViewSources(r *http.Request, st *store.Store, v *store.SavedView, scope *store.ViewScope, input views.Input, result *views.Result) error {
	if len(v.Spec.Sources) == 0 {
		return nil
	}
	requestedAt := time.Now()
	result.Data = map[string]views.Data{}
	key := fmt.Sprintf("%p/%s/%s", st, v.Spec.Project, v.Spec.Name)
	value, _ := viewSourceLocks.LoadOrStore(key, &viewSourceLock{})
	lock := value.(*viewSourceLock)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	refresh := r.URL.Query().Get("refresh") == "true" && lock.completed.Before(requestedAt)
	ranScript := false
	defer func() {
		if ranScript {
			lock.completed = time.Now()
		}
	}()
	for name, source := range v.Spec.Sources {
		switch source.From {
		case "record":
			records := []views.Record{}
			var ts *int64
			for _, record := range input.Records {
				if source.Section == "" || source.Section == record.Section {
					records = append(records, record)
					if ts == nil || record.TS > *ts {
						x := record.TS
						ts = &x
					}
				}
			}
			state := "ok"
			if ts == nil {
				state = "never"
			}
			recordSpec := v.Spec
			recordSpec.Sources = nil
			recordSpec.Section = source.Section
			recordSpec.Group = nil
			rendered, err := views.Render(recordSpec, views.Input{Records: records, Sources: input.Sources, Extras: input.Extras}, time.Now().Unix())
			if err != nil {
				return err
			}
			result.Data[name] = views.Data{State: state, UpdatedAt: ts, Value: rendered.Rows}
		case "report":
			value, err := s.viewSeries(r.Context(), scope, source.Metric, source.Points, nil)
			if err != nil {
				return err
			}
			result.Data[name] = views.Data{State: "ok", Value: value}
		case "script":
			cached, ran, err := st.ViewSourceCache(r.Context(), v.Spec.Project, v.Spec.Name, name)
			if err != nil {
				return err
			}
			every := 10 * time.Minute
			if source.Every != "" {
				every, _ = time.ParseDuration(source.Every)
			}
			if refresh || ran == 0 || time.Since(time.Unix(ran, 0)) >= every {
				runner, ok := s.backend.(interface {
					RunViewScript(context.Context, string, views.DataSource) (any, string, error)
				})
				if !ok {
					return fmt.Errorf("script sources unavailable")
				}
				data, stderr, runErr := runner.RunViewScript(r.Context(), v.Spec.Project, source)
				ranScript = true
				detail := ""
				if runErr != nil {
					detail = runErr.Error()
					if stderr != "" {
						detail += "\n" + stderr
					}
				}
				if err = st.SaveViewSourceCache(r.Context(), v.Spec.Project, v.Spec.Name, name, data, detail); err != nil {
					return err
				}
				cached, _, err = st.ViewSourceCache(r.Context(), v.Spec.Project, v.Spec.Name, name)
				if err != nil {
					return err
				}
			}
			result.Data[name] = cached
		}
	}
	return nil
}
