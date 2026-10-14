package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/logfile"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

func viewPath(project, name string) string {
	return "/api/v1/projects/" + url.PathEscape(project) + "/views/" + url.PathEscape(name)
}
func (p *Proxy) ListViews(ctx context.Context, project string) ([]store.ViewSummary, error) {
	path := "/api/v1/views"
	if project != "" {
		path = "/api/v1/projects/" + url.PathEscape(project) + "/views"
	}
	var out []store.ViewSummary
	err := p.do(ctx, "GET", path, nil, &out)
	return out, err
}
func (p *Proxy) SaveView(ctx context.Context, v store.SavedView) error {
	return p.do(ctx, "PUT", viewPath(v.Spec.Project, v.Spec.Name), v, nil)
}
func (p *Proxy) GetView(ctx context.Context, project, name string) (*store.SavedView, error) {
	var v store.SavedView
	err := p.do(ctx, "GET", viewPath(project, name), nil, &v)
	return &v, err
}
func (p *Proxy) DeleteView(ctx context.Context, project, name string) error {
	return p.do(ctx, "DELETE", viewPath(project, name), nil, nil)
}
func (p *Proxy) RenderView(ctx context.Context, project, name string, jobs []string, refresh bool) (*views.Result, error) {
	q := url.Values{}
	for _, id := range jobs {
		q.Add("job", id)
	}
	if refresh {
		q.Set("refresh", "true")
	}
	var out views.Result
	err := p.do(ctx, "GET", viewPath(project, name)+"/render?"+q.Encode(), nil, &out)
	return &out, err
}
func (p *Proxy) AddConsumer(ctx context.Context, id string, cfg consumer.Config) (*consumer.Consumer, error) {
	var c consumer.Consumer
	err := p.do(ctx, "POST", "/api/v1/jobs/"+url.PathEscape(id)+"/consumers", cfg, &c)
	return &c, err
}
func (p *Proxy) ListConsumers(ctx context.Context, id string) ([]consumer.Consumer, error) {
	var out []consumer.Consumer
	err := p.do(ctx, "GET", "/api/v1/jobs/"+url.PathEscape(id)+"/consumers", nil, &out)
	return out, err
}
func (p *Proxy) GetConsumer(ctx context.Context, id string) (*consumer.Consumer, error) {
	var c consumer.Consumer
	err := p.do(ctx, "GET", "/api/v1/consumers/"+url.PathEscape(id), nil, &c)
	return &c, err
}
func (p *Proxy) KillConsumer(ctx context.Context, id string) error {
	return p.do(ctx, "POST", "/api/v1/consumers/"+url.PathEscape(id)+"/kill", nil, nil)
}
func (p *Proxy) ConsumerLog(ctx context.Context, id string, req logfile.PageRequest) (*backend.LogPage, error) {
	q := url.Values{"max_bytes": {strconv.FormatInt(req.MaxBytes, 10)}}
	if !req.Tail {
		q.Set("offset", fmt.Sprint(req.Offset))
	}
	var page backend.LogPage
	err := p.do(ctx, "GET", "/api/v1/consumers/"+url.PathEscape(id)+"/log?"+q.Encode(), nil, &page)
	return &page, err
}
